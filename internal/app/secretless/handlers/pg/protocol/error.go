/*
Copyright 2017 Crunchy Data Solutions, Inc.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package protocol

import (
	"fmt"
)

/* PG Error Severity Levels */
const (
	ErrorSeverityFatal   string = "FATAL"
	ErrorSeverityPanic   string = "PANIC"
	ErrorSeverityWarning string = "WARNING"
	ErrorSeverityNotice  string = "NOTICE"
	ErrorSeverityDebug   string = "DEBUG"
	ErrorSeverityInfo    string = "INFO"
	ErrorSeverityLog     string = "LOG"
)

/* PG Error Message Field Identifiers */
const (
	ErrorFieldSeverity      byte = 'S'
	ErrorFieldCode          byte = 'C'
	ErrorFieldMessage       byte = 'M'
	ErrorFieldMessageDetail byte = 'D'
	ErrorFieldMessageHint   byte = 'H'
)

const (
	// ErrorCodeInternalError indicates an unspecified internal error.
	ErrorCodeInternalError = "XX000"
)

// Error is a Postgresql processing error.
type Error struct {
	Severity string
	Code     string
	Message  string
	Detail   string
	Hint     string
}

func (e *Error) Error() string {
	return fmt.Sprintf("pg: %s: %s", e.Severity, e.Message)
}

// GetMessage formats an Error into a protocol message.
func (e *Error) GetMessage() []byte {
	msg := NewMessageBuffer([]byte{})

	msg.WriteByte(ErrorMessageType)
	msg.WriteInt32(0)

	msg.WriteByte(ErrorFieldSeverity)
	msg.WriteString(e.Severity)

	msg.WriteByte(ErrorFieldCode)
	msg.WriteString(e.Code)

	msg.WriteByte(ErrorFieldMessage)
	msg.WriteString(e.Message)

	if e.Detail != "" {
		msg.WriteByte(ErrorFieldMessageDetail)
		msg.WriteString(e.Detail)
	}

	if e.Hint != "" {
		msg.WriteByte(ErrorFieldMessageHint)
		msg.WriteString(e.Hint)
	}

	msg.WriteByte(0x00) // null terminate the message

	msg.ResetLength(PGMessageLengthOffset)

	return msg.Bytes()
}
