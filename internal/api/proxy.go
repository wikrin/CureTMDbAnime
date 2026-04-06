package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"curetmdbanime/internal/net"
)

// 定义 HTTP 头部名称常量
const (
	HeaderContentLength    = "Content-Length"
	HeaderTransferEncoding = "Transfer-Encoding"
	HeaderHost             = "Host"
	XForwardedHost         = "X-Forwarded-Host"
	HeaderUserAgent        = "User-Agent"
)

// 定义错误消息常量
const (
	ErrMsgCreateUpstreamRequestFailed = "创建上游请求失败"
	ErrMsgParseProxyURLFailed         = "解析代理 URL 失败"
	ErrMsgProxyRequestFailed          = "代理请求失败"
	ErrMsgCopyResponseBodyFailed      = "复制响应体失败"
)

// 代理所有传入的客户端请求到配置的上游 TMDB URL
func ProxyHandler(c *gin.Context) {
	requestURI := c.Request.RequestURI
	upstreamURL := fmt.Sprintf("%s%s", config.AppSettings.TmdbAPIURL, requestURI)
	query := c.Request.URL.RawQuery

	// 创建上游请求, 流式传输请求体
	req, err := http.NewRequest(c.Request.Method, upstreamURL, c.Request.Body)
	if err != nil {
		logger.Error("%s: %v", ErrMsgCreateUpstreamRequestFailed, err)
		c.String(http.StatusBadGateway, ErrMsgCreateUpstreamRequestFailed)
		return
	}

	// 复制查询参数
	req.URL.RawQuery = query

	// 复制客户端请求头, 过滤 Content-Length, Transfer-Encoding 和 Host
	req.Header = make(http.Header)
	for header, values := range c.Request.Header {
		if !strings.EqualFold(header, HeaderContentLength) &&
			!strings.EqualFold(header, HeaderTransferEncoding) &&
			!strings.EqualFold(header, HeaderHost) &&
			!strings.EqualFold(header, XForwardedHost) {
			for _, value := range values {
				req.Header.Add(header, value)
			}
		}
	}

	// 使用 HTTP 客户端发送代理请求
	resp, err := net.GetHTTPClientWrapper().Client.Do(req)
	if err != nil {
		logger.Error("%s: %v", ErrMsgProxyRequestFailed, err)
		c.String(http.StatusBadGateway, ErrMsgProxyRequestFailed)
		return
	}
	defer resp.Body.Close()

	// 复制上游响应头部, 过滤 Content-Length 和 Transfer-Encoding
	for header, values := range resp.Header {
		if !strings.EqualFold(header, HeaderContentLength) &&
			!strings.EqualFold(header, HeaderTransferEncoding) {
			for _, value := range values {
				c.Writer.Header().Add(header, value)
			}
		}
	}

	// 设置响应状态码
	c.Status(resp.StatusCode)

	// 流式复制响应体
	written, err := io.Copy(c.Writer, resp.Body)
	if err != nil {
		logger.Error("%s: statusCode=%d bytes=%d err=%v", ErrMsgCopyResponseBodyFailed, resp.StatusCode, written, err)
		return
	}
}
