package model

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/mitchellh/mapstructure"
)

type IntPair struct {
	Season  int
	Episode int
}

// decodeWithMapstructure 使用 mapstructure 解码 map[string]any 到目标结构体
func decodeWithMapstructure(rawData map[string]any, result any) error {
	err := mapstructure.Decode(rawData, &result)
	if err != nil {
		return fmt.Errorf("mapstructure.Decode 解码失败: %w", err)
	}
	return nil
}

// mergeAndMarshal 合并已知字段和额外数据, 然后编码为 JSON
func mergeAndMarshal(alias any, other map[string]any) ([]byte, error) {
	// 初步编码已知字段
	knownFieldsBytes, err := json.Marshal(alias)
	if err != nil {
		return nil, err
	}

	// 将已知字段解组到 map 中
	knownFieldsMap := make(map[string]any)
	err = json.Unmarshal(knownFieldsBytes, &knownFieldsMap)
	if err != nil {
		return nil, err
	}

	finalMap := make(map[string]any)

	// 如果存在 "Other" 字段，将其内容解开并复制到 finalMap
	// 这些键值对具有最低优先级
	if ad, ok := knownFieldsMap["Other"]; ok {
		if adMap, isMap := ad.(map[string]any); isMap {
			maps.Copy(finalMap, adMap)
		}
		// 从 knownFieldsMap 中移除 Other 键
		delete(knownFieldsMap, "Other")
	}

	// 将传入的 other 复制到 finalMap
	// 这些键值对具有中等优先级，可能会覆盖 alias.Other 中的同名键
	maps.Copy(finalMap, other)

	// 将 knownFieldsMap 中剩余的字段复制到 finalMap
	// 这些键值对具有最高优先级，会覆盖之前的 alias.Other 和 other 中的同名键
	maps.Copy(finalMap, knownFieldsMap)

	// 编码为 JSON 并返回
	return json.Marshal(finalMap)
}
