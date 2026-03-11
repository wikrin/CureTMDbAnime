package model

import (
	"curetmdbanime/internal/logger"
	"fmt"
)

// ErrorResponse 定义统一的错误响应结构
type ErrorResponse struct {
	Success       bool   `json:"success"`        // 是否成功
	StatusCode    int    `json:"status_code"`    // 错误码
	StatusMessage string `json:"status_message"` // 错误信息
}

// NewErrorResponse 创建 ErrorResponse 实例
func NewErrorResponse(success bool, code int, message string) ErrorResponse {
	return ErrorResponse{
		Success:       success,
		StatusCode:    code,
		StatusMessage: message,
	}
}

// ServiceError 定义服务层错误结构体
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

// NewServiceError 创建 ServiceError 实例
func NewServiceError(code int, message string, err error) *ServiceError {
	se := &ServiceError{
		Code:    code,
		Message: message,
		Err:     err,
	}
	// 同时记录错误信息
	logger.Error("%s", se.Error())
	return se
}

// Error 实现 error 接口，提供错误字符串表示
func (se *ServiceError) Error() string {
	if se.Err != nil {
		return fmt.Sprintf("服务错误: 状态码=%d, 消息=%s, 原始错误=%v", se.Code, se.Message, se.Err)
	}
	return fmt.Sprintf("服务错误: 状态码=%d, 消息=%s", se.Code, se.Message)
}

// Unwrap 返回原始错误
func (se *ServiceError) Unwrap() error {
	return se.Err
}
