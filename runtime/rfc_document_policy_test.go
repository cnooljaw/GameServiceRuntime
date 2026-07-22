package gsr_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRFCMetadataPolicy(t *testing.T) {
	repositoryRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	rfcDirectory := filepath.Join(repositoryRoot, "docs", "rfcs")
	entries, err := os.ReadDir(rfcDirectory)
	if err != nil {
		t.Fatal(err)
	}

	allowedStatuses := map[string]bool{
		"草案":  true,
		"待实现": true,
		"已接受": true,
		"已废弃": true,
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join(rfcDirectory, entry.Name())
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(content), "\n")
			for index, line := range lines {
				if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
					t.Errorf("line %d has trailing whitespace", index+1)
				}
			}

			metadata := parseRFCMetadata(t, lines)
			status := requireRFCMetadata(t, metadata, "状态")
			if status != "" && !allowedStatuses[status] {
				t.Errorf("状态 = %q, want one of 草案/待实现/已接受/已废弃", status)
			}
			requireRFCMetadata(t, metadata, "范围")
			requireRFCMetadata(t, metadata, "依赖")
			if (status == "草案" || status == "待实现") && strings.TrimSpace(metadata["目标阶段"]) == "" {
				t.Error("草案或待实现 RFC 缺少目标阶段")
			}
			if acceptedAt, exists := metadata["接受日期"]; exists {
				if status != "已接受" {
					t.Errorf("接受日期只能用于已接受 RFC，当前状态 = %q", status)
				}
				if _, err := time.Parse("2006-01-02", acceptedAt); err != nil {
					t.Errorf("接受日期 = %q, want YYYY-MM-DD", acceptedAt)
				}
			}
		})
	}
}

func parseRFCMetadata(t *testing.T, lines []string) map[string]string {
	t.Helper()
	metadata := make(map[string]string)
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "# ") {
		t.Error("RFC 必须以单个 H1 开始")
		return metadata
	}
	for _, line := range lines[2:] {
		if !strings.HasPrefix(line, ">") {
			break
		}
		text := strings.TrimPrefix(line, ">")
		text = strings.TrimSpace(text)
		if text == "" {
			t.Error("RFC 元数据中不得出现空引用行")
			continue
		}
		key, value, ok := strings.Cut(text, "：")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			t.Errorf("无效 RFC 元数据行 %q", line)
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if _, exists := metadata[key]; exists {
			t.Errorf("RFC 元数据 %q 重复", key)
			continue
		}
		metadata[key] = value
	}
	return metadata
}

func requireRFCMetadata(t *testing.T, metadata map[string]string, key string) string {
	t.Helper()
	value := strings.TrimSpace(metadata[key])
	if value == "" {
		t.Errorf("RFC 元数据缺少 %q", key)
	}
	return value
}
