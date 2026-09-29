package workerlib

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type cliSegment struct {
	Text           string   `json:"text"`
	AudioStartTime *float64 `json:"audio_start_time"`
	AudioEndTime   *float64 `json:"audio_end_time"`
}

type Segment struct {
	Seq    int32
	StartS float64
	EndS   float64
	Text   string
}

func parseTranscripts(path string) ([]Segment, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read transcripts.json: %w", err)
	}
	var cliSegments []cliSegment
	if err := json.Unmarshal(raw, &cliSegments); err != nil {
		return nil, fmt.Errorf("decode transcripts.json: %w", err)
	}
	segments := make([]Segment, 0, len(cliSegments))
	for _, s := range cliSegments {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		start := valueOr(s.AudioStartTime, 0)
		end := valueOr(s.AudioEndTime, start)
		if end < start {
			end = start
		}
		segments = append(segments, Segment{StartS: start, EndS: end, Text: text})
	}
	sort.SliceStable(segments, func(i, j int) bool {
		return segments[i].StartS < segments[j].StartS
	})
	for i := range segments {
		segments[i].Seq = int32(i)
	}
	return segments, nil
}

func valueOr(ptr *float64, def float64) float64 {
	if ptr == nil {
		return def
	}
	return *ptr
}

func transcriptDurationSeconds(segments []Segment) float64 {
	var max float64
	for _, s := range segments {
		if s.EndS > max {
			max = s.EndS
		}
	}
	return max
}
