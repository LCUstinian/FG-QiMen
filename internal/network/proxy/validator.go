// validator.go — 4-stage proxy connection validation.
// validator.go — 4 阶段代理连接验证。
//
// Inspired by fscan's deep connection verification (L527-598), this
// validator performs:
// 1. Banner stage: check if connection is alive
// 2. HTTP probe: send GET request
// 3. Response analysis: check for full-echo proxy
// 4. Final verdict: determine if proxy is reliable
//
// 借鉴 fscan 的深度连接验证（L527-598），本验证器执行：
// 1. Banner 阶段：检查连接是否存活
// 2. HTTP 探针：发送 GET 请求
// 3. 响应分析：检查全回显代理
// 4. 最终判定：确定代理是否可靠
package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Validator validates proxy connections.
// Validator 验证代理连接。
type Validator struct {
	dialer  Dialer
	timeout time.Duration
}

// NewValidator creates a new Validator.
// NewValidator 创建新的 Validator。
func NewValidator(dialer Dialer, timeout time.Duration) *Validator {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Validator{
		dialer:  dialer,
		timeout: timeout,
	}
}

// ValidationResult holds validation results.
// ValidationResult 存放验证结果。
type ValidationResult struct {
	// IsAlive indicates if the connection was established.
	// IsAlive 指示连接是否建立。
	IsAlive bool

	// IsReliable indicates if the proxy is reliable (not full-echo).
	// IsReliable 指示代理是否可靠（非全回显）。
	IsReliable bool

	// Error is the validation error (if any).
	// Error 是验证错误（如有）。
	Error error

	// Stage is the stage where validation stopped.
	// Stage 是验证停止的阶段。
	Stage string
}

// Validate performs 4-stage validation on the proxy.
// Validate 对代理执行 4 阶段验证。
func (v *Validator) Validate(ctx context.Context, targetHost string, targetPort int) *ValidationResult {
	result := &ValidationResult{}

	// Stage 1: Banner check / 阶段 1：Banner 检查
	result.Stage = "banner"
	conn, err := v.dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", targetHost, targetPort))
	if err != nil {
		result.Error = fmt.Errorf("stage 1 (banner): %w", err)
		return result
	}
	defer conn.Close()

	result.IsAlive = true

	// Stage 2: HTTP probe / 阶段 2：HTTP 探针
	result.Stage = "http_probe"
	if err := v.sendHTTPProbe(conn); err != nil {
		result.Error = fmt.Errorf("stage 2 (http_probe): %w", err)
		return result
	}

	// Stage 3: Response analysis / 阶段 3：响应分析
	result.Stage = "response_analysis"
	isFullEcho, err := v.analyzeResponse(conn)
	if err != nil {
		result.Error = fmt.Errorf("stage 3 (response_analysis): %w", err)
		return result
	}

	// Stage 4: Final verdict / 阶段 4：最终判定
	result.Stage = "final_verdict"
	result.IsReliable = !isFullEcho

	return result
}

// sendHTTPProbe sends a simple HTTP GET request.
// sendHTTPProbe 发送简单的 HTTP GET 请求。
func (v *Validator) sendHTTPProbe(conn net.Conn) error {
	conn.SetWriteDeadline(time.Now().Add(v.timeout))
	defer conn.SetWriteDeadline(time.Time{})

	probe := "GET / HTTP/1.1\r\nHost: test\r\nUser-Agent: FG-QiMen-Validator\r\n\r\n"
	_, err := conn.Write([]byte(probe))
	return err
}

// analyzeResponse checks if the proxy is a full-echo proxy.
// analyzeResponse 检查代理是否是全回显代理。
//
// A full-echo proxy returns the exact request data back, which indicates
// it's not a real proxy but a transparent reflector (TUN mode, etc.).
//
// 全回显代理把请求原样返回，表明它不是真正的代理而是透明反射器（TUN 模式等）。
func (v *Validator) analyzeResponse(conn net.Conn) (bool, error) {
	conn.SetReadDeadline(time.Now().Add(v.timeout))
	defer conn.SetReadDeadline(time.Time{})

	reader := bufio.NewReader(conn)

	// Read first line / 读取首行
	firstLine, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for full-echo patterns / 检查全回显模式
	// A real HTTP response starts with "HTTP/1.x"
	// Full-echo returns "GET / HTTP/1.1"
	//
	// 真正的 HTTP 响应以 "HTTP/1.x" 开头
	// 全回显返回 "GET / HTTP/1.1"
	firstLine = strings.TrimSpace(firstLine)
	if strings.HasPrefix(firstLine, "GET ") || strings.HasPrefix(firstLine, "POST ") {
		return true, nil // Full-echo detected
	}

	return false, nil
}

// IsProxyReliable is a convenience function to validate a proxy.
// IsProxyReliable 是验证代理的便捷函数。
func IsProxyReliable(ctx context.Context, dialer Dialer, targetHost string, targetPort int, timeout time.Duration) bool {
	validator := NewValidator(dialer, timeout)
	result := validator.Validate(ctx, targetHost, targetPort)
	return result.IsReliable
}
