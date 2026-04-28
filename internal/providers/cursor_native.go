package providers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"
)

const (
	cursorWireVarint = 0
	cursorWireLen    = 2

	cursorTopRequestField = 1
	cursorReqMessages     = 1
	cursorReqModel        = 5
	cursorReqConversation = 23
	cursorReqMetadata     = 26
	cursorReqUnifiedMode  = 46
	cursorMsgContent      = 1
	cursorMsgRole         = 2
	cursorMsgID           = 13
	cursorModelName       = 1
	cursorModelEmpty      = 4
	cursorMetaPlatform    = 1
	cursorMetaArch        = 2
	cursorMetaVersion     = 3
	cursorMetaTimestamp   = 5

	cursorToolFieldID       = 1
	cursorResponseFieldID   = 2
	cursorToolIDField       = 3
	cursorToolNameField     = 9
	cursorToolArgsField     = 10
	cursorToolIsLastField   = 11
	cursorToolIsLastAlt     = 15
	cursorResponseTextField = 1
	cursorThinkingField     = 25
	cursorThinkingTextField = 1
)

type cursorDecodedResponse struct {
	Text     string
	Thinking string
	ToolCall *ToolCall
	IsLast   bool
}

func cursorGenerateBody(messages []ChatMessage, model string) []byte {
	request := []byte{}
	for i, msg := range messages {
		role := 1
		if msg.Role == "assistant" {
			role = 2
		}
		content := cursorMessageText(msg.Content)
		if content == "" && msg.Role == "user" {
			content = "continue"
		}
		message := cursorProtoJoin(
			cursorEncodeField(cursorMsgContent, cursorWireLen, []byte(content)),
			cursorEncodeField(cursorMsgRole, cursorWireVarint, uint64(role)),
			cursorEncodeField(cursorMsgID, cursorWireLen, []byte(fmt.Sprintf("msg_%d", i+1))),
		)
		request = cursorProtoJoin(request, cursorEncodeField(cursorReqMessages, cursorWireLen, message))
	}
	modelMsg := cursorProtoJoin(
		cursorEncodeField(cursorModelName, cursorWireLen, []byte(model)),
		cursorEncodeField(cursorModelEmpty, cursorWireLen, []byte{}),
	)
	metadata := cursorProtoJoin(
		cursorEncodeField(cursorMetaPlatform, cursorWireLen, []byte(cursorPlatformName(runtime.GOOS))),
		cursorEncodeField(cursorMetaArch, cursorWireLen, []byte(cursorArchName(runtime.GOARCH))),
		cursorEncodeField(cursorMetaVersion, cursorWireLen, []byte("3.1.0")),
		cursorEncodeField(cursorMetaTimestamp, cursorWireLen, []byte(time.Now().UTC().Format(time.RFC3339))),
	)
	request = cursorProtoJoin(
		request,
		cursorEncodeField(cursorReqModel, cursorWireLen, modelMsg),
		cursorEncodeField(cursorReqConversation, cursorWireLen, []byte(fmt.Sprintf("conv_%d", time.Now().UnixNano()))),
		cursorEncodeField(cursorReqMetadata, cursorWireLen, metadata),
		cursorEncodeField(cursorReqUnifiedMode, cursorWireVarint, uint64(1)),
	)
	top := cursorEncodeField(cursorTopRequestField, cursorWireLen, request)
	return cursorWrapConnectRPCFrame(top)
}

func cursorWrapConnectRPCFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = 0
	frame[1] = byte(len(payload) >> 24)
	frame[2] = byte(len(payload) >> 16)
	frame[3] = byte(len(payload) >> 8)
	frame[4] = byte(len(payload))
	copy(frame[5:], payload)
	return frame
}

func cursorEncodeField(fieldNum int, wireType int, value interface{}) []byte {
	tag := uint64(fieldNum<<3 | wireType)
	out := cursorEncodeVarint(tag)
	switch wireType {
	case cursorWireVarint:
		if v, ok := value.(uint64); ok {
			return cursorProtoJoin(out, cursorEncodeVarint(v))
		}
	case cursorWireLen:
		var data []byte
		switch v := value.(type) {
		case []byte:
			data = v
		case string:
			data = []byte(v)
		}
		return cursorProtoJoin(out, cursorEncodeVarint(uint64(len(data))), data)
	}
	return out
}

func cursorEncodeVarint(value uint64) []byte {
	out := make([]byte, 0, 10)
	for value >= 0x80 {
		out = append(out, byte(value)|0x80)
		value >>= 7
	}
	out = append(out, byte(value))
	return out
}

func cursorProtoJoin(parts ...[]byte) []byte {
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	out := make([]byte, 0, total)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func cursorMessageText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, raw := range v {
			part, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

type cursorConnectFrame struct {
	Flags    byte
	Payload  []byte
	Consumed int
}

func cursorCleanAccessToken(accessToken string) string {
	if idx := strings.Index(accessToken, "::"); idx >= 0 && idx+2 < len(accessToken) {
		return accessToken[idx+2:]
	}
	return accessToken
}

func cursorGenerateHashed64Hex(input, salt string) string {
	sum := sha256.Sum256([]byte(input + salt))
	return hex.EncodeToString(sum[:])
}

func cursorGenerateSessionID(accessToken string) string {
	clean := cursorCleanAccessToken(accessToken)
	return cursorGenerateHashed64Hex(clean, "cursor-session")[:32]
}

func cursorGenerateChecksum(machineID string, now time.Time) string {
	ts := now.Unix() / 1000000
	buf := []byte{
		byte(ts >> 40),
		byte(ts >> 32),
		byte(ts >> 24),
		byte(ts >> 16),
		byte(ts >> 8),
		byte(ts),
	}
	key := byte(165)
	for i := range buf {
		buf[i] = ((buf[i] ^ key) + byte(i%256)) & 0xFF
		key = buf[i]
	}
	return base64.RawURLEncoding.EncodeToString(buf) + machineID
}

func cursorBuildHeaders(accessToken, machineID string, ghostMode bool, now time.Time) (map[string]string, error) {
	cleanToken := strings.TrimSpace(cursorCleanAccessToken(accessToken))
	if cleanToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	effectiveMachineID := strings.TrimSpace(machineID)
	if effectiveMachineID == "" {
		effectiveMachineID = cursorGenerateHashed64Hex(cleanToken, "machineId")
	}
	configVersion := make([]byte, 16)
	requestID := make([]byte, 16)
	traceID := make([]byte, 16)
	_, _ = rand.Read(configVersion)
	_, _ = rand.Read(requestID)
	_, _ = rand.Read(traceID)
	ghostModeValue := "false"
	if ghostMode {
		ghostModeValue = "true"
	}
	return map[string]string{
		"authorization":               "Bearer " + cleanToken,
		"connect-accept-encoding":    "gzip",
		"connect-protocol-version":   "1",
		"content-type":               "application/connect+proto",
		"user-agent":                 "connect-es/1.6.1",
		"x-amzn-trace-id":            "Root=" + hex.EncodeToString(traceID),
		"x-client-key":               cursorGenerateHashed64Hex(cleanToken, ""),
		"x-cursor-checksum":          cursorGenerateChecksum(effectiveMachineID, now),
		"x-cursor-client-version":    "3.1.0",
		"x-cursor-client-type":       "ide",
		"x-cursor-client-os":         cursorPlatformName(runtime.GOOS),
		"x-cursor-client-arch":       cursorArchName(runtime.GOARCH),
		"x-cursor-client-device-type": "desktop",
		"x-cursor-config-version":    hex.EncodeToString(configVersion),
		"x-cursor-timezone":          "UTC",
		"x-ghost-mode":               ghostModeValue,
		"x-request-id":               hex.EncodeToString(requestID),
		"x-session-id":               cursorGenerateSessionID(cleanToken),
	}, nil
}

func cursorPlatformName(goos string) string {
	switch goos {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

func cursorArchName(goarch string) string {
	switch goarch {
	case "arm64":
		return "aarch64"
	case "amd64":
		return "x64"
	default:
		return goarch
	}
}

func cursorParseConnectRPCFrame(buffer []byte) (cursorConnectFrame, bool) {
	if len(buffer) < 5 {
		return cursorConnectFrame{}, false
	}
	length := int(buffer[1])<<24 | int(buffer[2])<<16 | int(buffer[3])<<8 | int(buffer[4])
	if len(buffer) < 5+length {
		return cursorConnectFrame{}, false
	}
	return cursorConnectFrame{
		Flags:    buffer[0],
		Payload:  append([]byte(nil), buffer[5:5+length]...),
		Consumed: 5 + length,
	}, true
}

func cursorExtractResponse(payload []byte) cursorDecodedResponse {
	fields := cursorDecodeMessage(payload)
	out := cursorDecodedResponse{}
	if toolPayload, ok := firstLenField(fields[cursorToolFieldID]); ok {
		toolFields := cursorDecodeMessage(toolPayload)
		id, _ := firstStringField(toolFields[cursorToolIDField])
		name, _ := firstStringField(toolFields[cursorToolNameField])
		args, _ := firstStringField(toolFields[cursorToolArgsField])
		isLast, ok := firstVarintField(toolFields[cursorToolIsLastField])
		if !ok {
			isLast, _ = firstVarintField(toolFields[cursorToolIsLastAlt])
		}
		if id != "" && name != "" {
			out.ToolCall = &ToolCall{
				ID:   strings.Split(id, "\n")[0],
				Type: "function",
				Function: &FunctionCall{
					Name:      name,
					Arguments: json.RawMessage(args),
				},
			}
			if len(out.ToolCall.Function.Arguments) == 0 {
				out.ToolCall.Function.Arguments = json.RawMessage(`{}`)
			}
			out.IsLast = isLast != 0
		}
	}
	if responsePayload, ok := firstLenField(fields[cursorResponseFieldID]); ok {
		responseFields := cursorDecodeMessage(responsePayload)
		out.Text, _ = firstStringField(responseFields[cursorResponseTextField])
		if thinkingPayload, ok := firstLenField(responseFields[cursorThinkingField]); ok {
			thinkingFields := cursorDecodeMessage(thinkingPayload)
			out.Thinking, _ = firstStringField(thinkingFields[cursorThinkingTextField])
		}
	}
	return out
}

type cursorProtoField struct {
	WireType int
	Varint   uint64
	Bytes    []byte
}

func cursorDecodeMessage(data []byte) map[int][]cursorProtoField {
	fields := map[int][]cursorProtoField{}
	for offset := 0; offset < len(data); {
		tag, n := cursorDecodeVarint(data[offset:])
		if n <= 0 {
			break
		}
		offset += n
		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x07)
		field := cursorProtoField{WireType: wireType}
		switch wireType {
		case 0:
			value, n := cursorDecodeVarint(data[offset:])
			if n <= 0 {
				return fields
			}
			offset += n
			field.Varint = value
		case 2:
			length, n := cursorDecodeVarint(data[offset:])
			if n <= 0 {
				return fields
			}
			offset += n
			end := offset + int(length)
			if end > len(data) {
				return fields
			}
			field.Bytes = append([]byte(nil), data[offset:end]...)
			offset = end
		default:
			return fields
		}
		fields[fieldNum] = append(fields[fieldNum], field)
	}
	return fields
}

func cursorDecodeVarint(data []byte) (uint64, int) {
	var value uint64
	for i, b := range data {
		value |= uint64(b&0x7F) << (7 * i)
		if b < 0x80 {
			return value, i + 1
		}
	}
	return 0, 0
}

func firstLenField(fields []cursorProtoField) ([]byte, bool) {
	if len(fields) == 0 || fields[0].WireType != 2 {
		return nil, false
	}
	return fields[0].Bytes, true
}

func firstStringField(fields []cursorProtoField) (string, bool) {
	bytes, ok := firstLenField(fields)
	if !ok {
		return "", false
	}
	return string(bytes), true
}

func firstVarintField(fields []cursorProtoField) (uint64, bool) {
	if len(fields) == 0 || fields[0].WireType != 0 {
		return 0, false
	}
	return fields[0].Varint, true
}
