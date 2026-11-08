package report

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

var (
	kinds  = map[string]Kind{"": Text, "text": Text, "number": Number, "bytes": Bytes, "duration": Duration}
	styles = map[string]Style{"": Plain, "plain": Plain, "key": Key, "muted": Muted}
)

// doc: отчёт в JSON - так его пишут свои команды tim-<имя> и читает tim render. Блок - объект с одним
// ключом: heading, facts, table, findings, note или next.
type doc struct {
	Schema  string                       `json:"schema"`
	Title   string                       `json:"title"`
	Verdict *Verdict                     `json:"verdict"`
	Data    json.RawMessage              `json:"data"`
	Blocks  []map[string]json.RawMessage `json:"blocks"`
}

type docTable struct {
	Columns []struct {
		Title string `json:"title"`
		Kind  string `json:"kind"`
		Style string `json:"style"`
	} `json:"columns"`
	Rows [][]json.RawMessage `json:"rows"`
}

// Decode: отчёт из JSON. Без data в --json уходит сам документ, чтобы скрипт получил хоть что-то.
// Длительность в таблице - секунды.
func Decode(r io.Reader) (Report, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return Report{}, err
	}
	var d doc
	if err := json.Unmarshal(raw, &d); err != nil {
		return Report{}, fmt.Errorf("отчёт не JSON: %w", err)
	}
	out := Report{Schema: d.Schema, Title: d.Title, Data: d.Data}
	if d.Verdict != nil {
		out.Verdict = *d.Verdict
	}
	if len(d.Data) == 0 {
		out.Data = json.RawMessage(raw)
	}
	for i, b := range d.Blocks {
		if len(b) != 1 {
			return Report{}, fmt.Errorf("блок %d: нужен ровно один ключ", i+1)
		}
		for key, val := range b {
			block, err := decodeBlock(key, val)
			if err != nil {
				return Report{}, fmt.Errorf("блок %d (%s): %w", i+1, key, err)
			}
			out.Blocks = append(out.Blocks, block)
		}
	}
	return out, nil
}

func decodeBlock(key string, val json.RawMessage) (Block, error) {
	switch key {
	case "heading":
		var s string
		err := json.Unmarshal(val, &s)
		return Heading(s), err
	case "note":
		var s string
		err := json.Unmarshal(val, &s)
		return Note(s), err
	case "facts":
		var list []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
			Level Level  `json:"level"`
		}
		err := json.Unmarshal(val, &list)
		facts := make(Facts, 0, len(list))
		for _, f := range list {
			facts = append(facts, Fact{Key: f.Key, Value: f.Value, Level: f.Level})
		}
		return facts, err
	case "findings":
		var list Findings
		err := json.Unmarshal(val, &list)
		return list, err
	case "next":
		var list []struct {
			Text    string `json:"text"`
			Command string `json:"command"`
		}
		err := json.Unmarshal(val, &list)
		next := make(Next, 0, len(list))
		for _, s := range list {
			next = append(next, Step{Text: s.Text, Command: s.Command})
		}
		return next, err
	case "table":
		return decodeTable(val)
	}
	return nil, fmt.Errorf("неизвестный блок, есть heading, facts, table, findings, note, next")
}

func decodeTable(val json.RawMessage) (Block, error) {
	var t docTable
	if err := json.Unmarshal(val, &t); err != nil {
		return nil, err
	}
	var out Table
	for _, c := range t.Columns {
		kind, ok1 := kinds[c.Kind]
		style, ok2 := styles[c.Style]
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("колонка %q: kind - text, number, bytes, duration; style - plain, key, muted", c.Title)
		}
		out.Columns = append(out.Columns, Column{Title: c.Title, Kind: kind, Style: style})
	}
	for _, row := range t.Rows {
		cells := make([]any, len(row))
		for i, raw := range row {
			kind := Text
			if i < len(out.Columns) {
				kind = out.Columns[i].Kind
			}
			cell, err := decodeCell(raw, kind)
			if err != nil {
				return nil, err
			}
			cells[i] = cell
		}
		out.Rows = append(out.Rows, cells)
	}
	return out, nil
}

// decodeCell: строка - как есть; число - по виду колонки; объект {text, level} - ячейка со своим уровнем.
func decodeCell(raw json.RawMessage, kind Kind) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case float64:
		if kind == Duration {
			return time.Duration(x * float64(time.Second)), nil
		}
		if kind == Text {
			return fmt.Sprint(x), nil
		}
		return int64(x), nil
	case map[string]any:
		var m struct {
			Text  string `json:"text"`
			Level Level  `json:"level"`
		}
		err := json.Unmarshal(raw, &m)
		return Mark{Text: m.Text, Level: m.Level}, err
	}
	return fmt.Sprint(v), nil
}
