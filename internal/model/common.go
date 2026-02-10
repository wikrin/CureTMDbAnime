package model

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"

	"github.com/mitchellh/mapstructure"
)

type IntPair struct {
	Season  int
	Episode int
}

// decodeWithMapstructure 使用 mapstructure 解码 map[string]any 到目标结构体。
// 额外字段会被存储在目标结构体的 AdditionalData 字段中（如果存在）。
func decodeWithMapstructure(rawData map[string]any, result any) error {
	metadata := mapstructure.Metadata{}
	decoderConfig := &mapstructure.DecoderConfig{
		Metadata:         &metadata,
		Result:           result,
		TagName:          "json",
		WeaklyTypedInput: true,
	}

	decoder, err := mapstructure.NewDecoder(decoderConfig)
	if err != nil {
		return fmt.Errorf("mapstructure.NewDecoder 初始化失败: %w", err)
	}

	if err := decoder.Decode(rawData); err != nil {
		return fmt.Errorf("mapstructure.Decode 解码失败: %w", err)
	}

	// 尝试将未使用的键（额外字段）填充到 AdditionalData 字段
	// 这里使用反射来检查 result 是否有 AdditionalData 字段并进行赋值
	if len(metadata.Unused) > 0 {
		// 转换 result 为 reflect.Value
		val := reflect.ValueOf(result)
		// 如果 result 是指针，获取其指向的值
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}

		// 确保 val 是一个结构体
		if val.Kind() == reflect.Struct {
			additionalDataField := val.FieldByName("AdditionalData")
			// 检查是否存在 AdditionalData 字段且其类型为 map[string]any
			if additionalDataField.IsValid() && additionalDataField.CanSet() && additionalDataField.Type().String() == "map[string]interface {}" {
				// 初始化 AdditionalData map
				if additionalDataField.IsNil() {
					additionalDataField.Set(reflect.MakeMap(reflect.TypeFor[map[string]any]()))
				}
				// 填充 AdditionalData
				for _, key := range metadata.Unused {
					if rawValue, ok := rawData[key]; ok {
						additionalDataField.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(rawValue))
					}
				}
			}
		}
	}
	return nil
}

// mergeAndMarshal 合并已知字段和额外数据，然后编码为 JSON。
func mergeAndMarshal(alias any, additionalData map[string]any) ([]byte, error) {
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

	// 1. 如果 knownFieldsMap 中存在 "AdditionalData" 字段，将其内容解开并复制到 finalMap
	// 这些键值对具有最低优先级。
	if ad, ok := knownFieldsMap["AdditionalData"]; ok {
		if adMap, isMap := ad.(map[string]any); isMap {
			maps.Copy(finalMap, adMap)
		}
		// 从 knownFieldsMap 中移除 AdditionalData 键，因为它已经被处理并解开了。
		delete(knownFieldsMap, "AdditionalData")
	}

	// 2. 将传入的 additionalData 复制到 finalMap。
	// 这些键值对具有中等优先级，可能会覆盖 alias.AdditionalData 中的同名键。
	maps.Copy(finalMap, additionalData)

	// 3. 将 knownFieldsMap 中剩余的直接字段复制到 finalMap。
	// 这些键值对具有最高优先级，会覆盖之前的 alias.AdditionalData 和 additionalData 中的同名键。
	maps.Copy(finalMap, knownFieldsMap)

	// 最后将合并后的 finalMap 编码为 JSON 并返回
	return json.Marshal(finalMap)
}
