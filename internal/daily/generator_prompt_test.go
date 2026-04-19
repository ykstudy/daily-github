package daily

import (
	"strings"
	"testing"
)

func TestGenerateUserPromptRequestsModelToFetchTrendingInsteadOfInjectingData(t *testing.T) {
	t.Parallel()

	prompt := generateUserPrompt("2026-04-19", PromptContext{})

	if strings.Contains(prompt, "## GitHub Trending 参考数据") {
		t.Fatal("prompt still contains injected GitHub Trending section")
	}
	if !strings.Contains(prompt, "请自行获取或参考 GitHub Trending 的日榜、周榜、月榜") {
		t.Fatal("prompt should instruct model to fetch or reference GitHub Trending data")
	}
	if !strings.Contains(prompt, "如果无法确认某个项目是否来自 GitHub Trending") {
		t.Fatal("prompt should constrain how Trending source is labeled when uncertain")
	}
	if !strings.Contains(prompt, "Trending 参考 / 补充推荐（基于公开信息核验）") {
		t.Fatal("prompt should use the new source wording")
	}
	if strings.Contains(prompt, "### GitHub Trending 日榜（daily）") {
		t.Fatal("prompt should not render embedded daily trending entries")
	}
}

func TestGenerateUserPromptStillIncludesHistoryDeduplicationList(t *testing.T) {
	t.Parallel()

	prompt := generateUserPrompt("2026-04-19", PromptContext{HistoricalRepos: []string{"owner1/repo1", "owner2/repo2"}})

	if !strings.Contains(prompt, "以下项目已经推荐过，本次禁止再次推荐") {
		t.Fatal("prompt should include history deduplication heading")
	}
	if !strings.Contains(prompt, "- owner1/repo1") || !strings.Contains(prompt, "- owner2/repo2") {
		t.Fatal("prompt should include historical repos in the deduplication list")
	}
}
