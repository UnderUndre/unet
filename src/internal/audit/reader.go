package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"time"
)

type QueryParams struct {
	Offset int
	Limit  int
	Actor  string
	Action Action
	From   *time.Time
	To     *time.Time
}

type QueryResult struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
	HasMore bool    `json:"hasMore"`
}

func Query(path string, params QueryParams) (*QueryResult, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &QueryResult{Entries: []Entry{}, Total: 0, HasMore: false}, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		if params.Actor != "" && entry.ActorTokenName != params.Actor {
			continue
		}
		if params.Action != "" && entry.Action != params.Action {
			continue
		}
		if params.From != nil {
			ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if err != nil || ts.Before(*params.From) {
				continue
			}
		}
		if params.To != nil {
			ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if err != nil || ts.After(*params.To) {
				continue
			}
		}

		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp > entries[j].Timestamp
	})

	total := len(entries)
	hasMore := params.Offset+params.Limit < total

	start := params.Offset
	if start > total {
		start = total
	}
	end := start + params.Limit
	if end > total {
		end = total
	}

	result := entries[start:end]
	if result == nil {
		result = []Entry{}
	}

	return &QueryResult{
		Entries: result,
		Total:   total,
		HasMore: hasMore,
	}, nil
}
