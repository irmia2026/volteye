package extract

import (
	"regexp"
	"strings"
	"sync"
)

type Rule struct {
	ID       int64
	Name     string
	Keywords []string
	Regex    string
	Enabled  bool
}

type compiledRule struct {
	Rule
	re *regexp.Regexp
}

type Engine struct {
	mu    sync.RWMutex
	rules []compiledRule
}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) Load(rules []Rule) error {
	var compiled []compiledRule
	for _, r := range rules {
		cr := compiledRule{Rule: r}
		if r.Regex != "" {
			re, err := regexp.Compile(r.Regex)
			if err != nil {
				return err
			}
			cr.re = re
		}
		compiled = append(compiled, cr)
	}
	e.mu.Lock()
	e.rules = compiled
	e.mu.Unlock()
	return nil
}

func (e *Engine) Match(content string) []int64 {
	if content == "" {
		return nil
	}
	lower := strings.ToLower(content)
	e.mu.RLock()
	defer e.mu.RUnlock()
	var hits []int64
	for _, r := range e.rules {
		if !r.Enabled {
			continue
		}
		matched := false
		for _, kw := range r.Keywords {
			if kw != "" && strings.Contains(lower, strings.ToLower(kw)) {
				matched = true
				break
			}
		}
		if !matched && r.re != nil && r.re.MatchString(content) {
			matched = true
		}
		if matched {
			hits = append(hits, r.ID)
		}
	}
	return hits
}

func (e *Engine) RuleNames(ids []int64) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var names []string
	for _, id := range ids {
		for _, r := range e.rules {
			if r.ID == id {
				names = append(names, r.Name)
				break
			}
		}
	}
	return names
}

func (e *Engine) Count() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.rules)
}
