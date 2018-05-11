/*
MIT License

Copyright (c) 2017 Aleksandr Fedotov

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package protocol

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strconv"
)

var errInvalidPacketLength = errors.New("Protocol: Invalid packet length")
var errInvalidPacketType = errors.New("Protocol: Invalid packet type")
var errFieldTypeNotImplementedYet = errors.New("Protocol: Required field type not implemented yet")

type ErrResponse struct {
	Message string
}

func UnpackErrResponse(packet []byte) (*ErrResponse, error) {
	return nil, nil
}

// OkResponse represents packet sent from the server to the client to signal successful completion of a command
// https://dev.mysql.com/doc/dev/mysql-server/latest/page_protocol_basic_ok_packet.html
type OkResponse struct {
	PacketType   byte
	AffectedRows uint64
	LastInsertID uint64
	StatusFlags  uint16
	Warnings     uint16
}

// UnpackOkResponse decodes OK_Packet from server.
// Part of basic packet structure shown below.
//
// int<3> PacketLength
// int<1> PacketNumber
// int<1> PacketType (0x00 or 0xFE)
// int<lenenc> AffectedRows
// int<lenenc> LastInsertID
// ... more ...
func UnpackOkResponse(packet []byte) (*OkResponse, error) {

	// Min packet length = header(4 bytes) + PacketType(1 byte)
	if err := CheckPacketLength(5, packet); err != nil {
		return nil, err
	}

	r := bytes.NewReader(packet)

	// Skip packet header
	if _, err := SkipPacketHeader(r); err != nil {
		return nil, err
	}

	// Read header, validate OK
	packetType, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	if packetType != responseOk {
		return nil, errors.New("Malformed packet")
	}

	// Read affected rows (expected value: 0 for auth)
	affectedRows, err := ReadLenEncodedInteger(r)
	if err != nil {
		return nil, err
	}

	// Read last insert ID (expected value: 0 for auth)
	lastInsertID, err := ReadLenEncodedInteger(r)
	if err != nil {
		return nil, err
	}

	// Read status flags
	statusBuf := make([]byte, 2)
	if _, err := r.Read(statusBuf); err != nil {
		return nil, err
	}
	status := binary.LittleEndian.Uint16(statusBuf)

	// Read warnings
	warningsBuf := make([]byte, 2)
	if _, err := r.Read(warningsBuf); err != nil {
		return nil, err
	}
	warnings := binary.LittleEndian.Uint16(warningsBuf)

	return &OkResponse{
		PacketType:   packetType,
		AffectedRows: affectedRows,
		LastInsertID: lastInsertID,
		StatusFlags:  status,
		Warnings:     warnings}, nil
}

// HandshakeV10 represents sever's initial handshake packet
// See https://mariadb.com/kb/en/mariadb/1-connecting-connecting/#initial-handshake-packet
type HandshakeV10 struct {
	ProtocolVersion    byte
	ServerVersion      string
	ConnectionID       uint32
	ServerCapabilities uint32
	AuthPlugin         string
	Salt               []byte
}

// UnpackHandshakeV10 decodes initial handshake request from server.
// Basic packet structure shown below.
// See http://imysql.com/mysql-internal-manual/connection-phase-packets.html#packet-Protocol::HandshakeV10
//
// int<3> PacketLength
// int<1> PacketNumber
// int<1> ProtocolVersion
// string<NUL> ServerVersion
// int<4> ConnectionID
// string<8> AuthPluginDataPart1 (authentication seed)
// string<1> Reserved (always 0x00)
// int<2> ServerCapabilities (1st part)
// int<1> ServerDefaultCollation
// int<2> StatusFlags
// int<2> ServerCapabilities (2nd part)
// if capabilities & clientPluginAuth
// {
// 		int<1> AuthPluginDataLength
// }
// else
// {
//		int<1> 0x00
// }
// string<10> Reserved (all 0x00)
// if capabilities & clientSecureConnection
// {
// 		string[$len] AuthPluginDataPart2 ($len=MAX(13, AuthPluginDataLength - 8))
// }
// if capabilities & clientPluginAuth
// {
//		string[NUL] AuthPluginName
// }
func UnpackHandshakeV10(packet []byte) (*HandshakeV10, error) {
	r := bytes.NewReader(packet)

	// Skip packet header
	if _, err := SkipPacketHeader(r); err != nil {
		return nil, err
	}

	// Read ProtocolVersion
	protoVersion, _ := r.ReadByte()

	// Read ServerVersion
	serverVersion := ReadNullTerminatedString(r)

	// Read ConnectionID
	connectionIDBuf := make([]byte, 4)
	if _, err := r.Read(connectionIDBuf); err != nil {
		return nil, err
	}
	connectionID := binary.LittleEndian.Uint32(connectionIDBuf)

	// Read AuthPluginDataPart1
	var salt []byte
	salt8 := make([]byte, 8)
	if _, err := r.Read(salt8); err != nil {
		return nil, err
	}
	salt = append(salt, salt8...)

	// Skip filler
	if _, err := r.ReadByte(); err != nil {
		return nil, err
	}

	// Read ServerCapabilities
	serverCapabilitiesLowerBuf := make([]byte, 2)
	if _, err := r.Read(serverCapabilitiesLowerBuf); err != nil {
		return nil, err
	}

	// Skip ServerDefaultCollation and StatusFlags
	if _, err := r.Seek(3, io.SeekCurrent); err != nil {
		return nil, err
	}

	// Read ExServerCapabilities
	serverCapabilitiesHigherBuf := make([]byte, 2)
	if _, err := r.Read(serverCapabilitiesHigherBuf); err != nil {
		return nil, err
	}

	// Compose ServerCapabilities from 2 bufs
	var serverCapabilitiesBuf []byte
	serverCapabilitiesBuf = append(serverCapabilitiesBuf, serverCapabilitiesLowerBuf...)
	serverCapabilitiesBuf = append(serverCapabilitiesBuf, serverCapabilitiesHigherBuf...)
	serverCapabilities := binary.LittleEndian.Uint32(serverCapabilitiesBuf)

	// Get length of AuthnPluginDataPart2
	// or read in empty byte if not included
	var authPluginDataLength byte
	if serverCapabilities&clientPluginAuth > 0 {
		var err error
		authPluginDataLength, err = r.ReadByte()
		if err != nil {
			return nil, err
		}
	} else {
		if _, err := r.ReadByte(); err != nil {
			return nil, err
		}
	}

	// Skip reserved (all 0x00)
	if _, err := r.Seek(10, io.SeekCurrent); err != nil {
		return nil, err
	}

	// Get AuthnPluginDataPart2
	var numBytes int
	if serverCapabilities&clientSecureConnection != 0 {
		numBytes = int(authPluginDataLength) - 8
		if numBytes < 0 || numBytes > 13 {
			numBytes = 13
		}

		salt2 := make([]byte, numBytes)
		if _, err := r.Read(salt2); err != nil {
			return nil, err
		}

		// the last byte has to be 0, and is not part of the data
		if salt2[numBytes-1] != 0 {
			return nil, errors.New("Malformed packet")
		}
		salt = append(salt, salt2[:numBytes-1]...)
	}

	var authPlugin string
	if serverCapabilities&clientPluginAuth != 0 {
		authPlugin = ReadNullTerminatedString(r)
	}

	return &HandshakeV10{
		ProtocolVersion:    protoVersion,
		ServerVersion:      serverVersion,
		ConnectionID:       connectionID,
		ServerCapabilities: serverCapabilities,
		AuthPlugin:         authPlugin,
		Salt:               salt,
	}, nil
}

// HandshakeResponse41 represents handshake response packet sent by 4.1+ clients supporting clientProtocol41 capability,
// if the server announced it in its initial handshake packet.
// See http://imysql.com/mysql-internal-manual/connection-phase-packets.html#packet-Protocol::HandshakeResponse41
type HandshakeResponse41 struct {
	Header          []byte
	CapabilityFlags uint32
	MaxPacketSize   uint32
	ClientCharset   uint8
	Username        string
	AuthLength      int64
	AuthResponse    []byte
	Database        string
	PacketTail      []byte
}

// UnpackHandshakeResponse41 decodes handshake response packet send by client.
// TODO: Add packet struct comment
// TODO: Add packet length check
func UnpackHandshakeResponse41(packet []byte) (*HandshakeResponse41, error) {
	r := bytes.NewReader(packet)

	// Skip packet header (but save in struct)
	header, err := SkipPacketHeader(r)
	if err != nil {
		return nil, err
	}

	// Read CapabilityFlags
	clientCapabilitiesBuf := make([]byte, 4)
	if _, err := r.Read(clientCapabilitiesBuf); err != nil {
		return nil, err
	}
	capabilityFlags := binary.LittleEndian.Uint32(clientCapabilitiesBuf)

	// Check that the server is using protocol 4.1
	if capabilityFlags&clientProtocol41 == 0 {
		return nil, errors.New("Protocol mismatch")
	}

	// Read MaxPacketSize
	maxPacketSizeBuf := make([]byte, 4)
	if _, err := r.Read(maxPacketSizeBuf); err != nil {
		return nil, err
	}
	maxPacketSize := binary.LittleEndian.Uint32(maxPacketSizeBuf)

	// Read Charset
	charset, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	// Skip 23 byte buffer
	if _, err := r.Seek(23, io.SeekCurrent); err != nil {
		return nil, err
	}

	// Read Username
	username := ReadNullTerminatedString(r)

	// Read Auth
	var auth []byte
	var authLength int64
	if capabilityFlags&clientSecureConnection > 0 {
		authLengthByte, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		authLength = int64(authLengthByte)

		auth = make([]byte, authLength)
		if _, err := r.Read(auth); err != nil {
			return nil, err
		}
	} else {
		auth = ReadNullTerminatedBytes(r)
	}

	// Read Database
	var database string
	if capabilityFlags&clientConnectWithDB > 0 {
		database = ReadNullTerminatedString(r)
	}

	// check whether the auth method was specified
	if capabilityFlags&clientPluginAuth > 0 {
		authPluginName := ReadNullTerminatedString(r)

		if authPluginName != defaultAuthPluginName {
			return nil, errors.New("Error in server handshake")
		}
	}

	// get the rest of the packet
	var packetTail []byte
	remainingByteLen := r.Len()
	if remainingByteLen > 0 {
		packetTail = make([]byte, remainingByteLen)
		if _, err := r.Read(packetTail); err != nil {
			return nil, err
		}
	}

	return &HandshakeResponse41{
		Header:          header,
		CapabilityFlags: capabilityFlags,
		MaxPacketSize:   maxPacketSize,
		ClientCharset:   charset,
		Username:        username,
		AuthLength:      authLength,
		AuthResponse:    auth,
		Database:        database,
		PacketTail:      packetTail}, nil
}

// InjectCredentials takes in a HandshakeResponse41 from the client, the
// salt from the server, and a username / password, and uses the salt
// from the server handshake to inject the username / password credentials into
// the client handshake response
func InjectCredentials(clientHandshake *HandshakeResponse41, salt []byte, username string, password string) (err error) {

	authResponse, err := NativePassword(password, salt)
	if err != nil {
		return
	}

	// Reset the payload length for the packet
	payloadLengthDiff := uint32(len(username) - len(clientHandshake.Username))
	payloadLengthDiff += uint32(len(authResponse) - int(clientHandshake.AuthLength))

	clientHandshake.Header, err = UpdateHeaderPayloadLength(clientHandshake.Header, payloadLengthDiff)
	if err != nil {
		return
	}

	clientHandshake.Username = username
	clientHandshake.AuthLength = int64(len(authResponse))
	clientHandshake.AuthResponse = authResponse

	return
}

// PackHandshakeResponse41 takes in a HandshakeResponse41 object and
// returns a handshake response packet
func PackHandshakeResponse41(clientHandshake *HandshakeResponse41) (packet []byte, err error) {

	var buf bytes.Buffer

	// write the header (same as the original)
	buf.Write(clientHandshake.Header)

	// write the capability flags
	capabilityFlagsBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(capabilityFlagsBuf, clientHandshake.CapabilityFlags)
	buf.Write(capabilityFlagsBuf)

	// write max packet size
	maxPacketSizeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(maxPacketSizeBuf, clientHandshake.MaxPacketSize)
	buf.Write(maxPacketSizeBuf)

	// write 1 byte char set
	buf.WriteByte(clientHandshake.ClientCharset)

	// write string[23] reserved (all zero)
	for i := 0; i < 23; i++ {
		buf.WriteByte(0)
	}

	// write string username
	buf.WriteString(clientHandshake.Username)
	buf.WriteByte(0)

	// write auth
	if clientHandshake.CapabilityFlags&clientSecureConnection > 0 {
		if clientHandshake.AuthLength > 0 {
			buf.WriteByte(uint8(len(clientHandshake.AuthResponse)))
			buf.Write(clientHandshake.AuthResponse)
		} else {
			buf.WriteByte(0)
		}
	} else {
		buf.Write(clientHandshake.AuthResponse)
		buf.WriteByte(0)
	}

	// write database (if set)
	if clientHandshake.CapabilityFlags&clientConnectWithDB > 0 {
		buf.WriteString(clientHandshake.Database)
		buf.WriteByte(0)
	}

	// write auth plugin name
	buf.WriteString(defaultAuthPluginName)
	buf.WriteByte(0)

	// write tail of packet (if set)
	if len(clientHandshake.PacketTail) > 0 {
		buf.Write(clientHandshake.PacketTail)
	}

	packet = buf.Bytes()

	return
}

// QueryRequest represents COM_QUERY or COM_STMT_PREPARE command sent by client to server.
type QueryRequest struct {
	Query string // SQL query value
}

// UnpackQueryRequest decodes COM_QUERY and COM_STMT_PREPARE requests from client.
// Basic packet structure shown below.
// See https://mariadb.com/kb/en/mariadb/com_query/ and https://mariadb.com/kb/en/mariadb/com_stmt_prepare/
//
// int<3> PacketLength
// int<1> PacketNumber
// int<1> Command COM_QUERY (0x03) or COM_STMT_PREPARE (0x16)
// string<EOF> SQLStatement
func UnpackQueryRequest(packet []byte) (*QueryRequest, error) {

	// Min packet length = header(4 bytes) + command(1 byte) + SQLStatement(at least 1 byte)
	if len(packet) < 6 {
		return nil, errInvalidPacketLength
	}

	// Fifth byte is command
	if packet[4] != ComQuery && packet[4] != ComStmtPrepare {
		return nil, errInvalidPacketType
	}

	return &QueryRequest{ReadEOFLengthString(packet[5:])}, nil
}

// ComStmtPrepareOkResponse represents COM_STMT_PREPARE_OK response structure.
type ComStmtPrepareOkResponse struct {
	StatementID   uint32 // ID of prepared statement
	ParametersNum uint16 // Num of prepared parameters
}

// UnpackComStmtPrepareOkResponse decodes COM_STMT_PREPARE_OK response from MySQL server.
// Basic packet structure shown below.
// See https://mariadb.com/kb/en/mariadb/com_stmt_prepare/#COM_STMT_PREPARE_OK
//
// int<3> PacketLength
// int<1> PacketNumber
// int<1> Command COM_STMT_PREPARE_OK (0x00)
// int<4> StatementID
// int<2> NumberOfColumns
// int<2> NumberOfParameters
// string<1> <not used>
// int<2> NumberOfWarnings
func UnpackComStmtPrepareOkResponse(packet []byte) (*ComStmtPrepareOkResponse, error) {

	// Min packet length = header(4 bytes) + command(1 byte) + statementID(4 bytes)
	// + number of columns (2 bytes) + number of parameters (2 bytes)
	// + <not used> (1 byte) + number of warnings (2 bytes)
	if len(packet) < 16 {
		return nil, errInvalidPacketLength
	}

	// Fifth byte is command
	if packet[4] != responsePrepareOk {
		return nil, errInvalidPacketType
	}

	statementID := binary.LittleEndian.Uint32(packet[5:9])
	parametersNum := binary.LittleEndian.Uint16(packet[11:13])

	return &ComStmtPrepareOkResponse{StatementID: statementID, ParametersNum: parametersNum}, nil
}

// ComStmtExecuteRequest represents COM_STMT_EXECUTE request structure.
type ComStmtExecuteRequest struct {
	StatementID        uint32              // ID of prepared statement
	PreparedParameters []PreparedParameter // Slice of prepared parameters
}

// PreparedParameter structure represents single prepared parameter structure for COM_STMT_EXECUTE request.
type PreparedParameter struct {
	FieldType byte   // Type of prepared parameter. See https://mariadb.com/kb/en/mariadb/resultset/#field-types
	Flag      byte   // Unused
	Value     string // String value of any prepared parameter passed with COM_STMT_EXECUTE request
}

// UnpackComStmtExecuteRequest decodes COM_STMT_EXECUTE packet sent by MySQL client.
// Basic packet structure shown below.
// See https://mariadb.com/kb/en/mariadb/com_stmt_execute/
//
// int<3> PacketLength
// int<1> PacketNumber
// int<1> COM_STMT_EXECUTE (0x17)
// int<4> StatementID
// int<1> Flags
// int<4> IterationCount = 1
// if (ParamCount > 0)
// {
// 		byte<(ParamCount + 7) / 8> NullBitmap
// 		byte<1>: SendTypeToServer = 0 or 1
// 		if (SendTypeToServer)
//		{
// 			Foreach parameter
//			{
// 				byte<1>: FieldType
//				byte<1>: ParameterFlag
//			}
//		}
// 		Foreach parameter
//		{
// 			byte<n> BinaryParameterValue
//		}
// }
func UnpackComStmtExecuteRequest(packet []byte, paramsCount uint16) (*ComStmtExecuteRequest, error) {

	// Min packet length = header(4 bytes) + command(1 byte) + statementID(4 bytes)
	// + flags(1 byte) + iteration count(4 bytes)
	if err := CheckPacketLength(14, packet); err != nil {
		return nil, err
	}

	// Fifth byte is command
	if packet[4] != ComStmtExecute {
		return nil, errInvalidPacketType
	}

	r := bytes.NewReader(packet)

	// Skip packet header
	if _, err := SkipPacketHeader(r); err != nil {
		return nil, err
	}

	// Skip to statementID position
	if _, err := r.Seek(1, io.SeekCurrent); err != nil {
		return nil, err
	}

	// Read StatementID
	statementIDBuf := make([]byte, 4)
	if _, err := r.Read(statementIDBuf); err != nil {
		return nil, err
	}
	statementID := binary.LittleEndian.Uint32(statementIDBuf)

	// Skip to NullBitmap position
	if _, err := r.Seek(5, io.SeekCurrent); err != nil {
		return nil, err
	}

	// Make buffer for n=paramsCount prepared parameters
	parameters := make([]PreparedParameter, paramsCount)

	if paramsCount > 0 {
		nullBitmapLength := int64((paramsCount + 7) / 8)

		// Skip to SendTypeToServer position
		if _, err := r.Seek(nullBitmapLength, io.SeekCurrent); err != nil {
			return nil, err
		}

		// Read SendTypeToServer
		sendTypeToServer, err := r.ReadByte()
		if err != nil {
			return nil, err
		}

		if sendTypeToServer == 1 {
			for index := range parameters {

				// Read parameter FieldType and ParameterFlag
				parameterMeta := make([]byte, 2)
				if _, err := r.Read(parameterMeta); err != nil {
					return nil, err
				}

				parameters[index].FieldType = parameterMeta[0]
				parameters[index].Flag = parameterMeta[1]
			}
		}

		var fieldDecoderError error
		var fieldValue string

		for index, parameter := range parameters {
			switch parameter.FieldType {

			// MYSQL_TYPE_VAR_STRING (length encoded string)
			case fieldTypeString:
				fieldValue, fieldDecoderError = DecodeFieldTypeString(r)

			// MYSQL_TYPE_LONGLONG
			case fieldTypeLongLong:
				fieldValue, fieldDecoderError = DecodeFieldTypeLongLong(r)

			// MYSQL_TYPE_DOUBLE
			case fieldTypeDouble:
				fieldValue, fieldDecoderError = DecodeFieldTypeDouble(r)

			// Field with missing decoder
			default:
				return nil, errFieldTypeNotImplementedYet
			}

			// Return with first decoding error
			if fieldDecoderError != nil {
				return nil, fieldDecoderError
			}

			parameters[index].Value = fieldValue
			fieldValue = ""
		}
	}

	return &ComStmtExecuteRequest{StatementID: statementID, PreparedParameters: parameters}, nil
}

// DecodeFieldTypeString decodes MYSQL_TYPE_VAR_STRING field (length-encoded string)
// See https://mariadb.com/kb/en/mariadb/resultset/#field-types
func DecodeFieldTypeString(r *bytes.Reader) (string, error) {
	str, _, err := ReadLenEncodedString(r)

	// io.EOF is ok since reader may be empty already because of empty prepared parameter value
	if err != nil && err != io.EOF {
		return "", err
	}

	return str, nil
}

// DecodeFieldTypeLongLong decodes MYSQL_TYPE_LONGLONG field
// See https://mariadb.com/kb/en/mariadb/resultset/#field-types
func DecodeFieldTypeLongLong(r *bytes.Reader) (string, error) {
	var bigIntValue int64

	if err := binary.Read(r, binary.LittleEndian, &bigIntValue); err != nil {
		return "", nil
	}

	return strconv.FormatInt(bigIntValue, 10), nil
}

// DecodeFieldTypeDouble decodes MYSQL_TYPE_DOUBLE field
// See https://mariadb.com/kb/en/mariadb/resultset/#field-types
func DecodeFieldTypeDouble(r *bytes.Reader) (string, error) {
	// Read 8 bytes required for float64
	doubleLengthBuf := make([]byte, 8)
	if _, err := r.Read(doubleLengthBuf); err != nil {
		return "", err
	}

	doubleBits := binary.LittleEndian.Uint64(doubleLengthBuf)
	doubleValue := math.Float64frombits(doubleBits)

	return strconv.FormatFloat(doubleValue, 'f', doubleDecodePrecision, 64), nil
}

// GetLenEncodedIntegerSize returns bytes count for length encoded integer
// determined by it's 1st byte
func GetLenEncodedIntegerSize(firstByte byte) byte {
	switch firstByte {
	case 0xfc:
		return 2
	case 0xfd:
		return 3
	case 0xfe:
		return 8
	default:
		return 1
	}
}

// ReadLenEncodedInteger returns parsed length-encoded integer and it's offset.
// See https://mariadb.com/kb/en/mariadb/protocol-data-types/#length-encoded-integers
func ReadLenEncodedInteger(r *bytes.Reader) (value uint64, err error) {
	firstLenEncIntByte, err := r.ReadByte()
	if err != nil {
		return
	}

	switch firstLenEncIntByte {
	case 0xfb:
		value = 0

	case 0xfc:
		data := make([]byte, 2)
		_, err = r.Read(data)
		if err != nil {
			return
		}
		value = uint64(data[0]) | uint64(data[1])<<8

	case 0xfd:
		data := make([]byte, 3)
		_, err = r.Read(data)
		if err != nil {
			return
		}
		value = uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16

	case 0xfe:
		data := make([]byte, 8)
		_, err = r.Read(data)
		if err != nil {
			return
		}
		value = uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 |
			uint64(data[3])<<24 | uint64(data[4])<<32 | uint64(data[5])<<40 |
			uint64(data[6])<<48 | uint64(data[7])<<56

	default:
		value = uint64(firstLenEncIntByte)
	}

	return value, err
}

// ReadLenEncodedString returns parsed length-encoded string and it's length.
// Length-encoded strings are prefixed by a length-encoded integer which describes
// the length of the string, followed by the string value.
// See https://mariadb.com/kb/en/mariadb/protocol-data-types/#length-encoded-strings
func ReadLenEncodedString(r *bytes.Reader) (string, uint64, error) {
	strLen, _ := ReadLenEncodedInteger(r)

	strBuf := make([]byte, strLen)
	if _, err := r.Read(strBuf); err != nil {
		return "", 0, err
	}

	return string(strBuf), strLen, nil
}

// ReadEOFLengthString returns parsed EOF-length string.
// EOF-length strings are those strings whose length will be calculated by the packet remaining length.
// See https://mariadb.com/kb/en/mariadb/protocol-data-types/#end-of-file-length-strings
func ReadEOFLengthString(data []byte) string {
	return string(data)
}

// ReadNullTerminatedString reads bytes from reader until 0x00 byte
// See https://mariadb.com/kb/en/mariadb/protocol-data-types/#null-terminated-strings
func ReadNullTerminatedString(r *bytes.Reader) string {
	var str []byte
	for {
		//TODO: Check for error
		b, _ := r.ReadByte()
		if b == 0x00 {
			return string(str)
		} else {
			str = append(str, b)
		}
	}
}

// ReadNullTerminatedBytes reads bytes from reader until 0x00 byte
func ReadNullTerminatedBytes(r *bytes.Reader) (str []byte) {
	for {
		//TODO: Check for error
		b, _ := r.ReadByte()
		if b == 0x00 {
			return
		} else {
			str = append(str, b)
		}
	}
}

// SkipPacketHeader rewinds reader to packet payload
func SkipPacketHeader(r *bytes.Reader) (s []byte, e error) {
	s = make([]byte, 4)

	if _, e = r.Read(s); e != nil {
		return nil, e
	}

	return
}

// CheckPacketLength checks if packet length meets expected value
func CheckPacketLength(expected int, packet []byte) error {
	if len(packet) < expected {
		return errInvalidPacketLength
	}

	return nil
}

// NativePassword calculates native password expected by server in HandshakeResponse41
// https://dev.mysql.com/doc/internals/en/secure-password-authentication.html#packet-Authentication::Native41
// SHA1( password ) XOR SHA1( "20-bytes random data from server" <concat> SHA1( SHA1( password ) ) )
func NativePassword(password string, salt []byte) (nativePassword []byte, err error) {

	sha1 := sha1.New()
	sha1.Write([]byte(password))
	passwordSHA1 := sha1.Sum(nil)

	sha1.Reset()
	sha1.Write(passwordSHA1)
	hash := sha1.Sum(nil)

	sha1.Reset()
	sha1.Write(salt)
	sha1.Write(hash)
	randomSHA1 := sha1.Sum(nil)

	// nativePassword = passwordSHA1 ^ randomSHA1
	nativePassword = make([]byte, len(randomSHA1))
	for i := range randomSHA1 {
		nativePassword[i] = passwordSHA1[i] ^ randomSHA1[i]
	}

	return
}

// UpdateHeaderPayloadLength takes in a 4 byte header and a difference
// in length, and returns a new header
func UpdateHeaderPayloadLength(origHeader []byte, diff uint32) (header []byte, err error) {

	initialPayloadLength, err := ReadUint24(origHeader[0:3])
	if err != nil {
		return nil, err
	}
	updatedPayloadLength := initialPayloadLength + diff
	header = append(WriteUint24(updatedPayloadLength), origHeader[3])

	return
}

// ReadUint24 takes in a byte slice and returns a uint32
func ReadUint24(b []byte) (uint32, error) {
	if len(b) < 3 {
		return 0, errors.New("Invalid packet")
	}

	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16, nil
}

// WriteUint24 takes in a uint32 and returns a byte slice
func WriteUint24(u uint32) (b []byte) {
	b = make([]byte, 3)
	b[0] = byte(u)
	b[1] = byte(u >> 8)
	b[2] = byte(u >> 16)

	return
}
