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
)

// ErrInvalidPacketLength is for invalid packet lengths
var ErrInvalidPacketLength = errors.New("Protocol: Invalid packet length")

// ErrInvalidPacketType is for invalid packet types
var ErrInvalidPacketType = errors.New("Protocol: Invalid packet type")

// ErrFieldTypeNotImplementedYet is for field types that are not yet implemented
var ErrFieldTypeNotImplementedYet = errors.New("Protocol: Required field type not implemented yet")

// ErrResponse provides an object to store error messages
type ErrResponse struct {
	Message string
}

// NewErrResponse is not yet implemented
func NewErrResponse(packet []byte) (*ErrResponse, error) {
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

// NewOkResponse decodes OK_Packet from server.
// Part of basic packet structure shown below.
//
// int<3> PacketLength
// int<1> PacketNumber
// int<1> PacketType (0x00 or 0xFE)
// int<lenenc> AffectedRows
// int<lenenc> LastInsertID
// ... more ...
func NewOkResponse(packet []byte) (*OkResponse, error) {

	// Min packet length = header(4 bytes) + PacketType(1 byte)
	if err := CheckPacketLength(5, packet); err != nil {
		return nil, err
	}

	r := bytes.NewReader(packet)

	// Skip packet header
	if _, err := GetPacketHeader(r); err != nil {
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
type HandshakeV10 struct {
	Header                 []byte
	ProtocolVersion        *MySQLInt // 1
	ServerVersion          *MySQLString
	ConnectionID           *MySQLInt // 4
	Salt1                  *MySQLNString // 8
	ServerCapabilities     uint32
	ServerDefaultCollation *MySQLInt // 1
	StatusFlags            *MySQLInt // 2
	Salt2                  *MySQLNString // MAX 13
	AuthPlugin             *MySQLString
}

// Concatenated Salt
func (serverHandshake *HandshakeV10) Salt() []byte {
	result := make([]byte, 0)
	result = append(result, serverHandshake.Salt1.Bytes()...)
	result = append(result, serverHandshake.Salt2.Bytes()...)
	return result
}

// RemoveClientSSL removes Client SSL Capability from Server Handshake
func (serverHandshake *HandshakeV10) RemoveClientSSL() {
	serverHandshake.ServerCapabilities &^= ClientSSL
}

// SupportsSSL returns truthy if the serverHandshake has the ClientSSL capability
func (serverHandshake *HandshakeV10) SupportsSSL() bool {
	return serverHandshake.ServerCapabilities & ClientSSL != 0
}

func (serverHandshake *HandshakeV10) Pack() ([]byte, error) {
	if len(serverHandshake.Header) != 4 {
		return nil, errors.New("HandshakeV10#Pack: malformed header")
	}

	var buf bytes.Buffer

	// Write Header (same as the original), ensure 4 bytes
	_, err := buf.Write(serverHandshake.Header)
	if err != nil {
		return nil, err
	}

	// Write ProtocolVersion
	err = serverHandshake.ProtocolVersion.Pack(&buf)
	if err != nil {
		return nil, err
	}
	// Write ServerVersion
	err = serverHandshake.ServerVersion.Pack(&buf)
	if err != nil {
		return nil, err
	}

	// Write Connection ID
	err = serverHandshake.ConnectionID.Pack(&buf)
	if err != nil {
		return nil, err
	}

	// Write AuthnPluginDataPart1
	err = serverHandshake.Salt1.Pack(&buf)
	if err != nil {
		return nil, err
	}

	// Filler
	err = buf.WriteByte(0)
	if err != nil {
		return nil, err
	}

	// write ServerCapabilities
	_, err = buf.Write([]byte{
		byte(serverHandshake.ServerCapabilities),
		byte(serverHandshake.ServerCapabilities >> 8),
	})
	if err != nil {
		return nil, err
	}

	// Write ServerDefaultCollation
	err = serverHandshake.ServerDefaultCollation.Pack(&buf)
	if err != nil {
		return nil, err
	}

	// Write StatusFlags
	err = serverHandshake.StatusFlags.Pack(&buf)
	if err != nil {
		return nil, err
	}

	// write ExServerCapabilities
	_, err = buf.Write([]byte{
		byte(serverHandshake.ServerCapabilities >> 16),
		byte(serverHandshake.ServerCapabilities >> 24),
	})
	if err != nil {
		return nil, err
	}

	// Write AuthnPluginData length
	if serverHandshake.ServerCapabilities&ClientPluginAuth > 0 {
		err = buf.WriteByte(uint8(serverHandshake.Salt1.length + serverHandshake.Salt2.length + 1))
	} else {
		err = buf.WriteByte(0)
	}
	if err != nil {
		return nil, err
	}

	// Skip reserved (all 0x00)
	_, err = buf.Write(make([]byte, 10))
	if err != nil {
		return nil, err
	}

	// Write AuthnPluginDataPart2
	err = serverHandshake.Salt2.Pack(&buf)
	if err != nil {
		return nil, err
	}
	err = buf.WriteByte(0)
	if err != nil {
		return nil, err
	}

	// Write AuthPluginName
	if serverHandshake.ServerCapabilities&ClientPluginAuth != 0 {
		err = serverHandshake.AuthPlugin.Pack(&buf)
	}
	if err != nil {
		return nil, err
	}

	result := buf.Bytes()

	// Update payload length
	binary.LittleEndian.PutUint32(result, uint32(buf.Len() - 4))

	return result, nil
}


// NewHandshakeV10 decodes initial handshake request from server.
func NewHandshakeV10(packet []byte) (*HandshakeV10, error) {
	r := bytes.NewReader(packet)

	// Read packet header
	header, err := GetPacketHeader(r)
	if err != nil {
		return nil, err
	}

	// Read ProtocolVersion
	protoVersion, err := NewMySQLInt(r, 1)
	if err != nil {
		return nil, err
	}

	// Read ServerVersion
	serverVersion, err := NewMySQLString(r)
	if err != nil {
		return nil, err
	}

	// Read ConnectionID
	connectionID, err := NewMySQLInt(r, 4)
	if err != nil {
		return nil, err
	}

	// Read AuthPluginDataPart1
	salt1, err := NewMySQLNString(r, 8)
	if err != nil {
		return nil, err
	}

	// Skip filler
	if _, err := r.ReadByte(); err != nil {
		return nil, err
	}

	// Read ServerCapabilities
	serverCapabilitiesLowerBuf := make([]byte, 2)
	if _, err := r.Read(serverCapabilitiesLowerBuf); err != nil {
		return nil, err
	}

	// Read ServerDefaultCollation
	serverDefaultCollation, err := NewMySQLInt(r ,1)
	if err != nil {
		return nil, err
	}

	// Read StatusFlags
	statusFlags, err := NewMySQLInt(r ,2)
	if err != nil {
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
	var authPluginDataLength *MySQLInt
	if serverCapabilities&ClientPluginAuth > 0 {
		var err error
		authPluginDataLength, err = NewMySQLInt(r, 1)
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
	var salt2 *MySQLNString
	if serverCapabilities&ClientSecureConnection != 0 {
		numBytes = authPluginDataLength.Val() - 8
		if numBytes < 0 || numBytes > 13 {
			numBytes = 13
		}

		salt2, err = NewMySQLNString(r, numBytes - 1)
		if err != nil {
			return nil, err
		}

		// the last byte has to be 0, and is not part of the data
		emptyByte, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if emptyByte != 0 {
			return nil, errors.New("Malformed packet")
		}
	}

	var authPlugin *MySQLString
	if serverCapabilities&ClientPluginAuth != 0 {
		authPlugin, err = NewMySQLString(r)
		if err != nil {
			return nil, err
		}
	}

	return &HandshakeV10{
		Header:                 header,
		ProtocolVersion:        protoVersion,
		ServerVersion:          serverVersion,
		ServerDefaultCollation: serverDefaultCollation,
		StatusFlags:            statusFlags,
		ConnectionID:           connectionID,
		ServerCapabilities:     serverCapabilities,
		AuthPlugin:             authPlugin,
		Salt1:                   salt1,
		Salt2:                   salt2,
	}, nil
}

// writes Uint16 starting from a given position in a byte slice
func writeUint16(data []byte, pos int, value uint16) {
	data[pos] = byte(value)
	data[pos+1] = byte(value >> 8)
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
	AuthPluginName  string
	AuthResponse    []byte
	Database        string
	PacketTail      []byte
}

// NewHandshakeResponse41 decodes handshake response packet send by client.
// TODO: Add packet struct comment
// TODO: Add packet length check
func NewHandshakeResponse41(packet []byte) (*HandshakeResponse41, error) {
	r := bytes.NewReader(packet)

	// Skip packet header (but save in struct)
	header, err := GetPacketHeader(r)
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
	if capabilityFlags&ClientProtocol41 == 0 {
		return nil, errors.New("Client Protocol mismatch")
	}

	// client requesting SSL, we don't support it
	clientRequestedSSL := capabilityFlags&ClientSSL > 0
	if clientRequestedSSL {
		return nil, errors.New("SSL Protocol mismatch")
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
	if capabilityFlags&ClientSecureConnection > 0 {
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
	if capabilityFlags&ClientConnectWithDB > 0 {
		database = ReadNullTerminatedString(r)
	}

	// check whether the auth method was specified
	var authPluginName string
	if capabilityFlags&ClientPluginAuth > 0 {
		authPluginName = ReadNullTerminatedString(r)
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
		AuthPluginName:  authPluginName,
		AuthResponse:    auth,
		Database:        database,
		PacketTail:      packetTail}, nil
}

// DisableClientPluginAuth removes ClientPluginAuth to the capability flags of the client handshake
func (clientHandshake *HandshakeResponse41) DisableClientPluginAuth() {
	clientHandshake.CapabilityFlags &^= ClientPluginAuth
}

// EnableClientSecureConnection adds ClientSecureConnection to the capability flags of the client handshake
func (clientHandshake *HandshakeResponse41) EnableClientSecureConnection() {
	clientHandshake.CapabilityFlags |= ClientSecureConnection
}

// EnableSSL adds ClientSSL to the capability flags of the client handshake
func (clientHandshake *HandshakeResponse41) EnableSSL() {
	clientHandshake.CapabilityFlags |= ClientSSL
}

// InjectCredentials takes in the
// salt from the server, and a username / password, and uses the salt
// from the server handshake to inject the username / password credentials into
// the client handshake response (HandshakeResponse41)
func (clientHandshake *HandshakeResponse41) InjectCredentials(salt []byte, username string, password string) (err error) {

	authResponse, err := NativePassword(password, salt)
	if err != nil {
		return
	}

	// Reset the payload length for the packet
	payloadLengthDiff := int32(len(username) - len(clientHandshake.Username))
	payloadLengthDiff += int32(len(authResponse) - int(clientHandshake.AuthLength))
	payloadLengthDiff += int32(len(defaultAuthPluginName) - len(clientHandshake.AuthPluginName))

	clientHandshake.Header, err = UpdateHeaderPayloadLength(clientHandshake.Header, payloadLengthDiff)
	if err != nil {
		return
	}

	clientHandshake.Username = username
	clientHandshake.AuthLength = int64(len(authResponse))
	clientHandshake.AuthResponse = authResponse

	return
}

// Pack encodes a HandshakeResponse41 object to bytes
// returns a handshake response packet
func (clientHandshake *HandshakeResponse41) Pack() (packet []byte, err error) {

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
	if clientHandshake.CapabilityFlags&ClientSecureConnection > 0 {
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
	if clientHandshake.CapabilityFlags&ClientConnectWithDB > 0 {
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
		}

		str = append(str, b)
	}
}

func WriteNullTerminatedString(buf *bytes.Buffer, contents string) error {
	if _, err := buf.WriteString(contents); err != nil {
		return err
	}

	return buf.WriteByte(0)
}

// ReadNullTerminatedBytes reads bytes from reader until 0x00 byte
func ReadNullTerminatedBytes(r *bytes.Reader) (str []byte) {
	for {
		//TODO: Check for error
		b, _ := r.ReadByte()

		if b == 0x00 {
			return
		}

		str = append(str, b)
	}
}

// GetPacketHeader rewinds reader to packet payload
func GetPacketHeader(r *bytes.Reader) (s []byte, e error) {
	s = make([]byte, 4)

	if _, e = r.Read(s); e != nil {
		return nil, e
	}

	return
}

// CheckPacketLength checks if packet length meets expected value
func CheckPacketLength(expected int, packet []byte) error {
	if len(packet) < expected {
		return ErrInvalidPacketLength
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
func UpdateHeaderPayloadLength(origHeader []byte, diff int32) (header []byte, err error) {

	initialPayloadLength, err := ReadUint24(origHeader[0:3])
	if err != nil {
		return nil, err
	}
	updatedPayloadLength := int32(initialPayloadLength) + diff
	if updatedPayloadLength < 0 {
		return nil, errors.New("Malformed packet")
	}
	header = append(WriteUint24(uint32(updatedPayloadLength)), origHeader[3])

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
