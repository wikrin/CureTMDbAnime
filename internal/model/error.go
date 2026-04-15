package model

import (
	"fmt"
	"strings"
)

// 定义统一的错误响应结构
type ErrorResponse struct {
	Success       bool   `json:"success"`        // 是否成功
	StatusCode    int    `json:"status_code"`    // 错误码
	StatusMessage string `json:"status_message"` // 错误信息
}

// 创建 ErrorResponse 实例
func NewErrorResponse(success bool, code int, message string) ErrorResponse {
	return ErrorResponse{
		Success:       success,
		StatusCode:    code,
		StatusMessage: message,
	}
}

// 定义服务层错误结构体
type ServiceError struct {
	Code       int    `json:"code" mapstructure:"code"`                       // 响应状态码
	StatusCode int    `json:"status_code" mapstructure:"status_code"`         // tmdb API 响应码
	Message    string `json:"status_message" mapstructure:"status_message"`   // 响应状态信息
	Err        error  `json:"error,omitempty" mapstructure:"error,omitempty"` // 原始错误
}

func (t *ServiceError) Decode(data map[string]any) error {
	err := decodeWithMapstructure(data, t)
	if err != nil {
		return err
	}
	return nil
}

// 创建 ServiceError 实例
func NewServiceError(code int, message string, err error) *ServiceError {
	return &ServiceError{
		Code:    code,
		Message: strings.TrimSpace(message),
		Err:     err,
	}
}

// 实现 error 接口，提供错误字符串表示
func (se *ServiceError) Error() string {
	if se == nil {
		return "<nil>"
	}

	parts := []string{fmt.Sprintf("服务错误: 状态码=%d", se.Code)}
	if se.Message != "" {
		parts = append(parts, fmt.Sprintf("消息=%s", se.Message))
	}
	if se.Err != nil {
		errText := se.Err.Error()
		if se.Message == "" || errText != se.Message {
			parts = append(parts, fmt.Sprintf("原始错误=%s", errText))
		}
	}
	return strings.Join(parts, ", ")
}

// 返回原始错误
func (se *ServiceError) Unwrap() error {
	return se.Err
}
