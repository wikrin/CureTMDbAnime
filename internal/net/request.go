package net

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"curetmdbanime/internal/model"
)

type ClientOptions struct {
	UseProxy bool
}

var (
	proxiedHTTPClient *http.Client
	proxiedClientOnce sync.Once
	directHTTPClient  *http.Client
	directClientOnce  sync.Once
)

// 按代理策略返回共享的 HTTP 客户端
func GetHTTPClient(opts ClientOptions) *http.Client {
	if opts.UseProxy {
		proxiedClientOnce.Do(func() {
			proxiedHTTPClient = NewHTTPClient(opts)
		})
		return proxiedHTTPClient
	}

	directClientOnce.Do(func() {
		directHTTPClient = NewHTTPClient(opts)
	})
	return directHTTPClient
}

// 创建一个新的 HTTP 客户端
func NewHTTPClient(opts ClientOptions) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:          100,              // 最大空闲连接数
		IdleConnTimeout:       90 * time.Second, // 空闲连接超时
		TLSHandshakeTimeout:   10 * time.Second, // TLS 握手超时
		ExpectContinueTimeout: 1 * time.Second,  // Expect: 100-continue 头超时
	}

	if opts.UseProxy && config.AppSettings.Proxy != "" {
		proxyURL, err := url.Parse(config.AppSettings.Proxy)
		if err != nil {
			logger.Error("解析代理 URL 失败: %v", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// 使用指定 HTTP 客户端发送原始 HTTP 请求
func Request(ctx context.Context, httpClient *http.Client, method, requestURL string, headers map[string]string, body []byte) (*http.Response, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("HTTP 客户端为空")
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	for key, value := range headers {
		if !isRestrictedHeader(key) {
			req.Header.Set(key, value)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("执行 HTTP 请求失败: %w", err)
	}
	return resp, nil
}

// 检查请求头是否受限
func isRestrictedHeader(header string) bool {
	restricted := map[string]bool{
		"host":             true, // 主机名
		"content-length":   true, // 内容长度
		"x-forwarded-host": true, // X-Forwarded-Host
		"referer":          true, // 引用页
		"origin":           true, // 源
	}

	return restricted[strings.ToLower(header)]
}

// 执行 GET 请求并返回响应
func GetResponse(ctx context.Context, httpClient *http.Client, requestURL string, headers map[string]string) (*http.Response, error) {
	return Request(ctx, httpClient, http.MethodGet, requestURL, headers, nil)
}

// 读取并返回响应体字节切片，完成后关闭响应体
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	if resp.Body == nil {
		return nil, nil
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	return bodyBytes, nil
}

// 封装 HTTP 请求和 JSON 处理逻辑
type APIClient struct {
	httpClient *http.Client
}

// 创建并返回一个新的 APIClient 实例
func NewAPIClient(opts ClientOptions) *APIClient {
	return &APIClient{
		httpClient: GetHTTPClient(opts),
	}
}

// 向指定 URL 发送 HTTP 请求并处理响应
// 支持可选请求体和查询参数，返回原始响应体字节切片
//
// 参数:
//
//	ctx context.Context: 请求上下文，用于超时和取消
//	method string: HTTP 请求方法 (例如 "GET", "POST")
//	baseURL string: 请求基本 URL
//	endpoint string: API 端点，与 baseURL 拼接
//	body any: 可选请求体，非 nil 则 JSON 编码
//	params url.Values: 可选 URL 查询参数
//	headers map[string]string: 可选请求头
//
// 返回:
//
//	[]byte: 成功时，返回原始响应体字节切片
//	error: 请求失败、响应状态码非 200 或读取响应体失败时返回错误
func (c *APIClient) DoRequest(ctx context.Context, method, baseURL, endpoint string, body any, params url.Values, headers map[string]string) ([]byte, error) {
	reqURL := baseURL + endpoint
	if params != nil {
		reqURL = fmt.Sprintf("%s?%s", reqURL, params.Encode())
	}

	var reqBody []byte
	// 存在请求体则 JSON 编码
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			logger.Error("JSON 请求体编码失败: %v, 方法: %s, URL: %s", err, method, reqURL)
			return nil, fmt.Errorf("JSON 请求体编码失败: %w", err)
		}
		reqBody = jsonBody
	}

	if headers == nil {
		headers = make(map[string]string)
	}
	// 默认 Content-Type 为 application/json
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}

	resp, err := Request(ctx, c.httpClient, method, reqURL, headers, reqBody)
	if err != nil {
		logger.Error("API 请求失败: %v, 方法: %s, URL: %s", err, method, reqURL)
		return nil, fmt.Errorf("API 请求失败: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("API 请求失败，HTTP 响应为空")
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := ReadResponseBody(resp)
		if readErr != nil {
			logger.Error("读取非 200 响应体失败: %v, 方法: %s, URL: %s", readErr, method, reqURL)
			return nil, fmt.Errorf("API 请求失败，状态码: %d，读取响应体失败: %w", resp.StatusCode, readErr)
		}

		var apiErr model.ErrorResponse
		if unmarshalErr := json.Unmarshal(bodyBytes, &apiErr); unmarshalErr == nil && apiErr.StatusMessage != "" {
			logger.Error("API 请求失败，状态码: %d，错误信息: %s，方法: %s, URL: %s", resp.StatusCode, apiErr.StatusMessage, method, reqURL)
			return nil, fmt.Errorf("API 请求失败，状态码: %d，错误信息: %s", resp.StatusCode, apiErr.StatusMessage)
		}

		logger.Error("API 请求失败，状态码: %d，响应体: %s，方法: %s, URL: %s", resp.StatusCode, string(bodyBytes), method, reqURL)
		return nil, fmt.Errorf("API 请求失败，状态码: %d，响应体: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := ReadResponseBody(resp)
	if err != nil {
		logger.Error("读取响应体失败: %v, 方法: %s, URL: %s", err, method, reqURL)
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	return bodyBytes, nil
}

// 将字节切片解码为指定类型 T
//
// 参数:
//
//	bodyBytes []byte: 包含 JSON 响应的字节切片
//
// 返回:
//
//	T: 解码后的数据
//	error: JSON 解码失败时返回错误
func UnmarshalResponse[T any](bodyBytes []byte) (T, error) {
	var typedResult T
	if err := json.Unmarshal(bodyBytes, &typedResult); err != nil {
		logger.Error("JSON 响应体解码失败，响应内容: %s，错误: %v", bodyBytes, err)
		var zero T
		return zero, fmt.Errorf("JSON 响应体解码失败: %w", err)
	}
	return typedResult, nil
}
