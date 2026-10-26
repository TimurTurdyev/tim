package recipes

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	makeTarget = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_./-]*(?:\s+[A-Za-z0-9][A-Za-z0-9_./-]*)*)\s*::?(?:[^=]|$)`)
	justRecipe = regexp.MustCompile(`^@?([A-Za-z0-9][A-Za-z0-9_-]*)(?:\s+[^:]*)?:(?:[^=]|$)`)
	taskName   = regexp.MustCompile(`^  ([A-Za-z0-9][A-Za-z0-9_:.-]*):\s*(?:#.*)?$`)
	taskDesc   = regexp.MustCompile(`^\s{4,}desc:\s*(.+)$`)
)

// Task: задача проекта и чем она запускается.
type Task struct {
	Name string
	Tool string // make, just, task, npm, pnpm, yarn, bun, composer
	Desc string // комментарий рядом с целью или сама команда
}

// Args: команда запуска задачи.
func (t Task) Args() []string {
	switch t.Tool {
	case "npm", "pnpm", "bun":
		return []string{t.Tool, "run", t.Name}
	case "composer":
		return []string{"composer", "run-script", t.Name}
	}
	return []string{t.Tool, t.Name}
}

// Tasks: задачи проекта из всех файлов, что есть в папке; одноимённая задача берётся из первого.
func Tasks(dir string) []Task {
	var out []Task
	seen := map[string]bool{}
	add := func(list []Task) {
		for _, t := range list {
			if !seen[t.Name] {
				seen[t.Name] = true
				out = append(out, t)
			}
		}
	}
	for _, f := range []string{"justfile", "Justfile", ".justfile"} {
		if data, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			add(justTasks(data))
			break
		}
	}
	for _, f := range []string{"Taskfile.yml", "Taskfile.yaml"} {
		if data, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			add(taskfileTasks(data))
			break
		}
	}
	for _, f := range []string{"GNUmakefile", "Makefile", "makefile"} {
		if data, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			add(makeTasks(data))
			break
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		add(scriptTasks(data, nodeTool(dir), nil))
	}
	if data, err := os.ReadFile(filepath.Join(dir, "composer.json")); err == nil {
		add(scriptTasks(data, "composer", composerEvent))
	}
	return out
}

// makeTasks: цели Makefile; описание - "## текст" в строке цели или комментарий строкой выше.
// Служебные (.PHONY), шаблонные (%) и присваивания (X := 1) пропускаются.
func makeTasks(data []byte) []Task {
	var out []Task
	prev := ""
	for sc := bufio.NewScanner(bytes.NewReader(data)); sc.Scan(); {
		line := sc.Text()
		m := makeTarget.FindStringSubmatch(line)
		if m == nil || strings.Contains(line, "%") {
			prev = commentText(line)
			continue
		}
		desc := prev
		if _, after, ok := strings.Cut(line, "##"); ok {
			desc = strings.TrimSpace(after)
		}
		for _, name := range strings.Fields(m[1]) {
			out = append(out, Task{Name: name, Tool: "make", Desc: desc})
		}
		prev = ""
	}
	return out
}

// justTasks: рецепты justfile; описание - комментарий строкой выше; [private] и _имя не показываются.
func justTasks(data []byte) []Task {
	var out []Task
	prev, private := "", false
	for sc := bufio.NewScanner(bytes.NewReader(data)); sc.Scan(); {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "["):
			private = private || strings.Contains(line, "private")
			continue
		case strings.HasPrefix(line, "#"):
			prev = commentText(line)
			continue
		}
		m := justRecipe.FindStringSubmatch(line)
		if m != nil && !private && !strings.HasPrefix(m[1], "_") && !slices.Contains([]string{"set", "alias", "import", "export", "mod"}, m[1]) {
			out = append(out, Task{Name: m[1], Tool: "just", Desc: prev})
		}
		prev, private = "", false
	}
	return out
}

// taskfileTasks: ключи раздела tasks: в Taskfile и их desc - без разбора YAML целиком.
func taskfileTasks(data []byte) []Task {
	var out []Task
	in := false
	for sc := bufio.NewScanner(bytes.NewReader(data)); sc.Scan(); {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "tasks:"):
			in = true
		case line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
			in = false
		case !in:
		case taskName.MatchString(line):
			out = append(out, Task{Name: taskName.FindStringSubmatch(line)[1], Tool: "task"})
		case len(out) > 0 && out[len(out)-1].Desc == "" && taskDesc.MatchString(line):
			out[len(out)-1].Desc = strings.Trim(taskDesc.FindStringSubmatch(line)[1], `"'`)
		}
	}
	return out
}

// composerEvent: события composer (post-install-cmd, pre-autoload-dump) - это не задачи, их зовёт composer.
func composerEvent(name string) bool {
	return strings.HasPrefix(name, "pre-") || strings.HasPrefix(name, "post-")
}

// scriptTasks: "scripts" из package.json или composer.json в порядке файла; описание - сама команда.
func scriptTasks(data []byte, tool string, skip func(string) bool) []Task {
	var doc struct {
		Scripts json.RawMessage `json:"scripts"`
	}
	if json.Unmarshal(data, &doc) != nil || len(doc.Scripts) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(doc.Scripts))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var out []Task
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return out
		}
		var val any
		if dec.Decode(&val) != nil {
			return out
		}
		name, _ := key.(string)
		if skip != nil && skip(name) {
			continue
		}
		desc := ""
		switch v := val.(type) {
		case string:
			desc = v
		case []any:
			var parts []string
			for _, p := range v {
				if s, ok := p.(string); ok {
					parts = append(parts, s)
				}
			}
			desc = strings.Join(parts, "; ")
		}
		out = append(out, Task{Name: name, Tool: tool, Desc: desc})
	}
	return out
}

// nodeTool: менеджер пакетов по lock-файлу.
func nodeTool(dir string) string {
	for _, c := range []struct{ file, tool string }{{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lockb", "bun"}, {"bun.lock", "bun"}} {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			return c.tool
		}
	}
	return "npm"
}

func commentText(line string) string {
	if t, ok := strings.CutPrefix(strings.TrimSpace(line), "#"); ok {
		return strings.TrimSpace(strings.TrimLeft(t, "#"))
	}
	return ""
}
