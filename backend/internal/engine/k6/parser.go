package k6

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// maxParseSamples 解析样本上限（保护内存；典型压测样本远小于该值）。
const maxParseSamples = 2_000_000

// Sample k6 单点样本（`--out json` 的 Point 行）。
type Sample struct {
	Metric string
	Value  float64
	Time   time.Time
	Tags   map[string]string
}

// Result 解析后的引擎结果。
type Result struct {
	Samples   []Sample
	StartedAt time.Time
	EndedAt   time.Time
}

// jsonPoint k6 --out json 的行结构（Metric 行不含 data，忽略）。
type jsonPoint struct {
	Metric string `json:"metric"`
	Type   string `json:"type"`
	Data   struct {
		Time  time.Time         `json:"time"`
		Value float64           `json:"value"`
		Tags  map[string]string `json:"tags"`
	} `json:"data"`
}

// Parse 解析 k6 JSON 输出（逐行 JSON）。
func Parse(rd io.Reader) (*Result, error) {
	if rd == nil {
		return nil, errors.New("k6 结果为空")
	}
	res := &Result{Samples: make([]Sample, 0, 1024)}
	scanner := bufio.NewScanner(rd)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var point jsonPoint
		if err := json.Unmarshal(line, &point); err != nil {
			// 单行非法不阻断整体解析（k6 可能在输出中混入非 JSON 日志）。
			continue
		}
		if point.Type != "Point" || point.Metric == "" {
			continue
		}
		if len(res.Samples) >= maxParseSamples {
			return nil, fmt.Errorf("k6 结果样本数超过上限 %d", maxParseSamples)
		}
		t := point.Data.Time
		if res.StartedAt.IsZero() || (!t.IsZero() && t.Before(res.StartedAt)) {
			if !t.IsZero() {
				res.StartedAt = t
			}
		}
		if t.After(res.EndedAt) {
			res.EndedAt = t
		}
		res.Samples = append(res.Samples, Sample{
			Metric: point.Metric,
			Value:  point.Data.Value,
			Time:   t,
			Tags:   point.Data.Tags,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取 k6 结果失败: %w", err)
	}
	return res, nil
}

// ParseFile 解析 k6 结果文件。
func ParseFile(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 k6 结果失败: %w", err)
	}
	defer func() { _ = f.Close() }()
	return Parse(f)
}
