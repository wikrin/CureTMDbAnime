package providers

import (
	"testing"
)

func TestExtractSeasonNumber(t *testing.T) {
	// 创建一个 BangumiAPIClient 实例，以便访问 seasonNumberRegexes
	client := NewBangumiAPIClient()

	tests := []struct {
		name     string
		nameCN   string
		expected int
	}{
		// 中文季号
		{"", "番剧 第一季", 1},
		{"", "番剧 第二季", 2},
		{"", "番剧 第十一季", 11},
		{"", "番剧 第十二季", 12},
		{"", "番剧 第二十季", 20},
		{"", "番剧 第二十三季", 23},
		{"", "番剧 第三十五季", 35},
		{"", "番剧 第一期", 1},
		{"", "番剧 第七期", 7},
		{"", "番剧 第 伍 季", 5},
		{"", "番剧 第陆季", 6},
		{"", "番剧 拾季", 10},
		{"", "番剧 壹季", 1},

		// 英文季号
		{"Anime Season 1", "", 1},
		{"Anime Season 5", "", 5},
		{"Anime Season 12", "", 12},
		{"Anime S 3", "", 3},
		{"Anime S 04", "", 4},
		{"Season 7 Anime", "", 7},

		// 英文序数季号
		{"Anime 1st season", "", 1},
		{"Anime 2nd season", "", 2},
		{"Anime 10th season", "", 10},
		{"Anime 23rd season", "", 23},

		// 罗马数字季号
		{"Anime Season I", "", 1},
		{"Anime S II", "", 2},
		{"Anime Season V", "", 5},
		{"Anime Season X", "", 10},
		{"Anime Season XII", "", 12},

		// 混合大小写和空格
		{"anime SeasoN  01 ", "", 1},
		{"anime s VII", "", 7},

		// nameCN 优先
		{"Anime Season 2", "番剧 第一季", 1},
		{"Anime S III", "番剧 第二季", 2},

		// 没有季号
		{"Just Anime Title", "", DefaultSeasonNumber},
		{"", "仅有动漫标题", DefaultSeasonNumber},
		{"", "", DefaultSeasonNumber}, // 空字符串
		{"Another Title", "另一个标题", DefaultSeasonNumber},

		// 数字作为季号 (当没有明确的 "Season" 或 "季" 字样时，默认 SeasonNumber)
		{"Anime 123", "", DefaultSeasonNumber},
		{"", "动漫 456", DefaultSeasonNumber},
	}

	for _, tt := range tests {
		t.Run(tt.nameCN+" / "+tt.name, func(t *testing.T) {
			got := client.ExtractSeasonNumber(tt.name, tt.nameCN)
			if got != tt.expected {
				t.Errorf("ExtractSeasonNumber(%q, %q) = %v, want %v", tt.name, tt.nameCN, got, tt.expected)
			}
		})
	}
}

func TestNumberConverter(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"1", 1},
		{"10", 10},
		{"0", 0},
		{"一", 1},
		{"五", 5},
		{"十", 10},
		{"壹", 1},
		{"陆", 6},
		{"拾", 10},
		{"I", 1},
		{"V", 5},
		{"X", 10},
		{"II", 2},
		{"IV", 4},
		{"IX", 9},
		{"XII", 12},
		{"abc", 0},
		{"", 0},
		{"非法字符1", 0},
		{"十一", 11},
		{"十二", 12},
		{"二十", 20},
		{"二十三", 23},
		{"三十五", 35},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := numberConverter(tt.input)
			if got != tt.expected {
				t.Errorf("numberConverter(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
