package collection

// Set 是一个泛型集合类型
type Set[T comparable] map[T]struct{}

// NewSet 创建一个新的集合
func NewSet[T comparable](items ...T) Set[T] {
	s := make(Set[T])
	for _, item := range items {
		s.Add(item)
	}
	return s
}

// Add 添加元素到集合
func (s Set[T]) Add(item T) {
	s[item] = struct{}{}
}

// Contains 检查元素是否在集合中
func (s Set[T]) Contains(item T) bool {
	_, exists := s[item]
	return exists
}

// Union 返回两个集合的并集
func (s Set[T]) Union(other Set[T]) Set[T] {
	result := NewSet[T]()
	for item := range s {
		result.Add(item)
	}
	for item := range other {
		result.Add(item)
	}
	return result
}

// Intersection 返回两个集合的交集
func (s Set[T]) Intersection(other Set[T]) Set[T] {
	result := NewSet[T]()
	for item := range s {
		if other.Contains(item) {
			result.Add(item)
		}
	}
	return result
}

// Difference 返回两个集合的差集（s - other）
func (s Set[T]) Difference(other Set[T]) Set[T] {
	result := NewSet[T]()
	for item := range s {
		if !other.Contains(item) {
			result.Add(item)
		}
	}
	return result
}

// IsSubset 检查是否为子集
func (s Set[T]) IsSubset(other Set[T]) bool {
	for item := range s {
		if !other.Contains(item) {
			return false
		}
	}
	return true
}

// ToSlice 将集合转换为切片
func (s Set[T]) ToSlice() []T {
	result := make([]T, 0, len(s))
	for item := range s {
		result = append(result, item)
	}
	return result
}

// RenameKeysInPlace 原地重命名 Map 的键
func RenameKeysInPlace(m map[string]any, renames map[string]string) {
	for oldKey, newKey := range renames {
		if val, exists := m[oldKey]; exists {
			m[newKey] = val
			delete(m, oldKey)
		}
	}
}

// getString 从 map 中安全地获取 string 值
func GetString(data map[string]any, key string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	if ptr, ok := data[key].(*string); ok && ptr != nil {
		return *ptr
	}
	return ""
}

// getInt 从 map 中安全地获取 int 值
func GetInt(data map[string]any, key string) int {
	if val, ok := data[key].(float64); ok {
		return int(val)
	}
	if val, ok := data[key].(int); ok {
		return val
	}
	if ptr, ok := data[key].(*int); ok && ptr != nil {
		return *ptr
	}
	return -1
}

// getFloat64 从 map 中安全地获取 float64 值
func GetFloat64(data map[string]any, key string) float64 {
	if val, ok := data[key].(float64); ok {
		return val
	}
	if ptr, ok := data[key].(*float64); ok && ptr != nil {
		return *ptr
	}
	return 0.0
}

// getStringPtr 从 map 或 *string 中安全地获取 *string 值
func GetStringPtr(data any, key string) *string {
	if m, ok := data.(map[string]any); ok {
		if val, ok := m[key].(string); ok && val != "" {
			return &val
		}
	} else if strPtr, ok := data.(*string); ok && strPtr != nil && *strPtr != "" {
		return strPtr
	}
	return nil
}

// getSliceOfString 从 map 中安全地获取 []string 值
func GetSliceOfString(data map[string]any, key string) []string {
	if val, ok := data[key].([]any); ok {
		var result []string
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// getSliceOfInt 从 map 中安全地获取 []int 值
func GetSliceOfInt(data map[string]any, key string) []int {
	if val, ok := data[key].([]any); ok {
		var result []int
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				if id, idOk := m["id"].(float64); idOk {
					result = append(result, int(id))
				}
			} else if i, ok := item.(float64); ok {
				result = append(result, int(i))
			}
		}
		return result
	}
	return nil
}
