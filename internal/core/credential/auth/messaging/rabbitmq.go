// Package protocols: RabbitMQ authenticator.
//
// Strategy: AMQP 0-9-1 PLAIN auth flow. / 策略：AMQP 0-9-1 PLAIN
// 认证流程。
//   1. Send protocol header "AMQP\0\0\9\1" (8 bytes).
//   2. Server sends Connection.Start.
//   3. We send Connection.Start-Ok with credentials in PLAIN
//      format: long-string "\0user\0pass" (zero-padded).
//   4. Server replies with either Connection.Tune (hit) or
//      Connection.Close (miss).
//
// HARD RULE: on a hit we return. We do NOT declare exchanges,
// publish messages, or create queues.
//
// 包 protocols：RabbitMQ 认证器。
// 策略：AMQP 0-9-1 PLAIN 认证流程。1) 发协议头；2) 服务器发 Start；
// 3) 我们发 Start-Ok 凭据用 PLAIN 格式 "\0user\0pass"；4) 服务器回
// Tune（命中）或 Close（不命中）。
//
// 硬性原则：命中即返回。不声明 exchange、不发消息、不创建队列。
package messaging

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// RabbitMQAuthenticator authenticates against RabbitMQ via AMQP
// 0-9-1 PLAIN auth. / RabbitMQAuthenticator 通过 AMQP 0-9-1 PLAIN
// 认证对 RabbitMQ 认证。
//
// DefaultPorts returns 5672 (AMQP) / 15672 (Management HTTP — we
// don't auth that in v0.1). / DefaultPorts 返 5672（AMQP）/ 15672
//（管理 HTTP——v0.1 不认证那个）。
type RabbitMQAuthenticator struct{}

// NewRabbitMQAuthenticator returns a default RabbitMQ authenticator.
// NewRabbitMQAuthenticator 返回默认配置的 RabbitMQ 认证器。
func NewRabbitMQAuthenticator() *RabbitMQAuthenticator { return &RabbitMQAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *RabbitMQAuthenticator) Name() string { return "rabbitmq" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *RabbitMQAuthenticator) DefaultPorts() []int {
	return []int{5672}
}

// Authenticate implements credential.Authenticator. / Authenticate 实现
// credential.Authenticator。
func (a *RabbitMQAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		ok, err := a.attempt(ctx, addr, c.User, c.Pass, timeout)
		if err != nil {
			return nil, err
		}
		if ok {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// attempt runs one AMQP PLAIN auth round. / attempt 跑一次 AMQP PLAIN
// 认证。
func (a *RabbitMQAuthenticator) attempt(ctx context.Context, addr, user, pass string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	// 1. Protocol header. / 协议头。
	if _, err := conn.Write([]byte("AMQP\x00\x00\x09\x01")); err != nil {
		return false, err
	}
	// 2. Read Connection.Start (frame type 1, channel 0, method
	// class 10 method 11).
	// / 读 Connection.Start（frame 类型 1，channel 0，method class 10
	// method 11）。
	startFrame, err := readAMQPFrame(conn)
	if err != nil {
		return false, err
	}
	// Parse: skip type(1) + channel(2) + size(4) = 7 bytes, then
	// class(2) + method(2) = 4 bytes. We verify class 10 method 11.
	// / 解析：跳 type(1) + channel(2) + size(4) = 7 字节，然后
	// class(2) + method(2) = 4 字节。验证 class 10 method 11。
	if len(startFrame) < 11 {
		return false, nil
	}
	if binary.BigEndian.Uint16(startFrame[7:9]) != 0x000a {
		return false, nil
	}
	if binary.BigEndian.Uint16(startFrame[9:11]) != 0x000b {
		return false, nil
	}
	// 3. Send Start-Ok. / 发 Start-Ok。
	if err := sendAMQPStartOK(conn, "PLAIN", user, pass, "en_US"); err != nil {
		return false, err
	}
	// 4. Read next frame: Tune (hit) or Close (miss).
	// / 读下一帧：Tune（命中）或 Close（不命中）。
	respFrame, err := readAMQPFrame(conn)
	if err != nil {
		return false, nil
	}
	if len(respFrame) < 11 {
		return false, nil
	}
	class := binary.BigEndian.Uint16(respFrame[7:9])
	method := binary.BigEndian.Uint16(respFrame[9:11])
	// Connection.Tune = 10/0x1e. Connection.Close = 10/0x32.
	// / Connection.Tune = 10/0x1e。Connection.Close = 10/0x32。
	return class == 0x000a && method == 0x001e, nil
}

// sendAMQPStartOK builds and sends a Start-Ok frame with the given
// credentials in PLAIN format. / sendAMQPStartOK 构造并发 Start-Ok 帧，
// 用 PLAIN 格式带凭据。
//
// PLAIN format: long-string "\0user\0pass". / PLAIN 格式：长字符串
// "\0user\0pass"。
func sendAMQPStartOK(conn net.Conn, mechanism, user, pass, locale string) error {
	// Build PLAIN payload: 4-byte length + "\0" + user + "\0" + pass.
	// / 构造 PLAIN payload：4 字节长度 + "\0" + user + "\0" + pass。
	plain := "\x00" + user + "\x00" + pass
	plainBytes := []byte(plain)
	plainLen := make([]byte, 4)
	binary.BigEndian.PutUint32(plainLen, uint32(len(plainBytes)))
	// Build client-properties map: empty long-map (0x00 0x00 0x00 0x00).
	// / 构造 client-properties map：空长 map（0x00 0x00 0x00 0x00）。
	clientProps := []byte{0x00, 0x00, 0x00, 0x00}
	// Build mechanism: long-string mechanism. / 构造 mechanism：长
	// 字符串 mechanism。
	mechBytes := []byte(mechanism)
	mechLen := make([]byte, 4)
	binary.BigEndian.PutUint32(mechLen, uint32(len(mechBytes)))
	// Build response: long-string PLAIN bytes. / 构造 response：长字符
	// 串 PLAIN 字节。
	respLen := make([]byte, 4)
	binary.BigEndian.PutUint32(respLen, uint32(len(plainBytes)))
	// Build locale: short-string locale. / 构造 locale：短字符串 locale。
	locBytes := []byte(locale)
	locLen := byte(len(locBytes))
	// Concatenate. / 拼装。
	body := append([]byte{}, clientProps...)
	body = append(body, mechLen...)
	body = append(body, mechBytes...)
	body = append(body, respLen...)
	body = append(body, plainBytes...)
	body = append(body, locLen)
	body = append(body, locBytes...)
	// Frame: type(1)=1 + channel(2)=0 + size(4) + class(2)=10 +
	// method(2)=0x0b + body + 0xCE.
	// / 帧：type(1)=1 + channel(2)=0 + size(4) + class(2)=10 +
	// method(2)=0x0b + body + 0xCE。
	frame := make([]byte, 0, 16+len(body))
	frame = append(frame, 0x01, 0x00, 0x00)
	sizePos := len(frame)
	frame = append(frame, 0, 0, 0, 0)
	var classBuf [2]byte
	binary.BigEndian.PutUint16(classBuf[:], 0x000a)
	frame = append(frame, classBuf[:]...)
	var methodBuf [2]byte
	binary.BigEndian.PutUint16(methodBuf[:], 0x000b)
	frame = append(frame, methodBuf[:]...)
	frame = append(frame, body...)
	frame = append(frame, 0xCE)
	binary.BigEndian.PutUint32(frame[sizePos:sizePos+4], uint32(len(frame)))
	_, err := conn.Write(frame)
	return err
}

// readAMQPFrame reads one AMQP frame. / readAMQPFrame 读一个 AMQP 帧。
func readAMQPFrame(conn net.Conn) ([]byte, error) {
	hdr := make([]byte, 7)
	if _, err := readFullRMQ2(conn, hdr); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(hdr[3:7])
	if size < 7 || size > 16384 {
		return nil, fmt.Errorf("rmq: bad frame size %d", size)
	}
	// Frame is `size` bytes TOTAL including the 7-byte header we
	// already read. So we need `size-7` more bytes. / 帧总共 `size`
	// 字节，包括已读的 7 字节头。所以需要再读 `size-7` 字节。
	body := make([]byte, size-7)
	if _, err := readFullRMQ2(conn, body); err != nil {
		return nil, err
	}
	return append(hdr, body...), nil
}

func readFullRMQ2(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// init registers the RabbitMQ authenticator. / init 注册 RabbitMQ 认证器。
func init() {
	credential.Register(NewRabbitMQAuthenticator())
}
