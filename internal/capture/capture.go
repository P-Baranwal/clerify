// Package capture turns a short natural-language line such as
//
//	essay due friday, about 5h
//
// into the fields `add` needs. It is deliberately conservative: if the text
// has no date or effort cue it reports no match, so ordinary titles keep their
// exact wording.
package capture

import (
	"regexp"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// Result is what capture extracted. Nil fields mean "not mentioned".
type Result struct {
	Title          string
	Type           string
	Due            *string
	EstimatedHours *float64
}

var (
	hoursRE = regexp.MustCompile(`(?i)\b(?:about|around|roughly|approx\.?|for|est\.?|~)?\s*(\d+(?:\.\d+)?)\s*(?:hours?|hrs?|h)\b`)
	dateRE  = regexp.MustCompile(`(?i)\b(` +
		`today|tomorrow|tmrw|tmr|next week|` +
		`(?:this|next)\s+(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday|mon|tues|tue|weds|wed|thurs|thur|thu|fri|sat|sun)|` +
		`(?:monday|tuesday|wednesday|thursday|friday|saturday|sunday|mon|tues|tue|weds|wed|thurs|thur|thu|fri|sat|sun)|` +
		`\+\d+d?|in\s+\d+\s+days?|` +
		`\d{4}-\d{2}-\d{2}|` +
		`(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\s+\d{1,2}(?:,?\s+\d{4})?|` +
		`\d{1,2}\s+(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*` +
		`)\b`)
	dueWordRE = regexp.MustCompile(`(?i)\b(?:due|by|before)\b`)
	trailRE   = regexp.MustCompile(`(?i)\b(?:on|at|this|next)\s*$`)
)

// Parse extracts a title, type, deadline, and estimate. ok is false when the
// text has no date or effort cue.
func Parse(text string, now time.Time) (Result, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Result{}, false
	}

	work := text
	var out Result

	if m := hoursRE.FindStringSubmatch(work); m != nil {
		hours := parseFloat(m[1])
		if hours > 0 {
			out.EstimatedHours = &hours
			work = strings.Replace(work, m[0], " ", 1)
		}
	}

	if m := dateRE.FindString(work); m != "" {
		if due, err := model.ParseDeadlineInput(m, now); err == nil && due != "" {
			out.Due = &due
			work = strings.Replace(work, m, " ", 1)
		}
	}

	if out.Due == nil && out.EstimatedHours == nil {
		return Result{}, false
	}

	work = dueWordRE.ReplaceAllString(work, " ")
	work = trailRE.ReplaceAllString(strings.TrimSpace(work), " ")
	out.Title = cleanTitle(work)
	if out.Title == "" {
		return Result{}, false
	}
	out.Type = inferType(out.Title)
	return out, true
}

func cleanTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, " \t\n,;:-–—.")
	s = strings.TrimSpace(trailRE.ReplaceAllString(s, " "))
	s = strings.Trim(s, " \t\n,;:-–—.")
	return s
}

func inferType(title string) string {
	lower := strings.ToLower(title)
	switch {
	case strings.Contains(lower, "essay"):
		return model.TypeEssay
	case strings.Contains(lower, "math"), strings.Contains(lower, "problem set"):
		return model.TypeMath
	case strings.Contains(lower, "code"), strings.Contains(lower, "coding"), strings.Contains(lower, "program"),
		strings.Contains(lower, "bug"), strings.Contains(lower, "feature"):
		return model.TypeCoding
	case strings.Contains(lower, "research"), strings.Contains(lower, "paper"), strings.Contains(lower, "thesis"):
		return model.TypeResearch
	default:
		return ""
	}
}

func parseFloat(s string) float64 {
	var whole, frac float64
	var fracScale float64 = 1
	seenDot := false
	for _, r := range s {
		switch {
		case r == '.':
			seenDot = true
		case r >= '0' && r <= '9':
			if seenDot {
				fracScale *= 10
				frac = frac*10 + float64(r-'0')
			} else {
				whole = whole*10 + float64(r-'0')
			}
		default:
			return 0
		}
	}
	return whole + frac/fracScale
}
