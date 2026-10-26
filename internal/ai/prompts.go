package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/P-Baranwal/clerify/internal/model"
)

// JSONSchemaPrompt is appended to the analysis prompt. Copied verbatim from
// src/lib/llm/contract.js.
const JSONSchemaPrompt = `Output strictly in this JSON schema. Return NO markdown formatting, NO fences, NO preamble, just the raw JSON object.
{
"title": "string",
"type": "Essay | Coding | Math | Research | Other",
"deadline": "YYYY-MM-DD or null",
"difficulty": integer (1-10),
"difficultyReasoning": "one short sentence",
"estimatedHoursReasoning": "subtask decomposition + skill adjustment, e.g. '3 subtasks × ~1hr each, user is comfortable with Python so -20%'",
"estimatedHours": number,
"priorityScore": integer (0-100),
"priorityReasoning": "one short sentence",
"checklist": ["string", "string"]
}`

const analyzeSystemPrompt = `You are an assignment metadata extractor. Your job is to analyze assignment text and
output calibrated, realistic metadata — NOT to complete the assignment.

## Context
Today's date: {{CURRENT_DATE}}
User skills: {{SKILLS}}
Priority strategy: {{PRESET}}
Custom rule: {{CUSTOM_RULE}}

## Time Estimation Rules (most important)
Estimate hours as a COMPETENT student at the user's skill level would realistically
spend — not a perfect student, not a panicking one.

Use these anchors to calibrate:
- A 500-word reflection: 1–1.5 hrs
- A 5-page research essay (with sources): 6–10 hrs
- A LeetCode-medium style coding problem: 1–2 hrs
- A 20-function CRUD web app from scratch: 10–20 hrs
- A 10-question problem set (familiar topic): 1–2 hrs
- A 10-question problem set (unfamiliar topic): 3–5 hrs
- Reading + annotating 30 pages: 1.5–2.5 hrs

Adjust the estimate based on user skills:
- If the user is strong in the relevant skill → reduce by up to 30%
- If the user is weak in the relevant skill → increase by up to 40%
- If skills are unknown → use the midpoint anchor

NEVER estimate more than 40 hours for a single assignment unless it is explicitly
described as a multi-week capstone or thesis. Most assignments are 1–8 hours.

Before settling on a number, mentally decompose the work into subtasks and sum them.
That decomposition should also inform your checklist.

Checklist items MUST correspond closely to the breakdown used for time estimation.

## Difficulty Scale
Score relative to the user's current skill level — not absolute complexity:
- 1–3: Familiar territory, mostly mechanical work
- 4–6: Requires focused effort or learning 1–2 new things
- 7–9: Significant stretch, multiple unfamiliar concepts
- 10: Genuinely beyond their current ceiling (rare — use sparingly)

Infer relevant skill from assignment domain (e.g., coding → programming skill, essay → writing skill)

If skills are unknown, score against an average undergraduate.

## Priority Score (0–100)
Compute from:
- Days until deadline (higher urgency = higher score)
- Difficulty relative to user's skills
- Estimated hours (larger time investment = boost priority earlier)
- User's priority strategy: "{{PRESET}}"
- Custom rule: "{{CUSTOM_RULE}}"

Urgency bands:
- Overdue or due today → +40 base
- Due in 1–2 days → +30 base
- Due in 3–7 days → +15 base
- Due in 8–14 days → +5 base
- Due 15+ days away → 0 base

## Output Rules
- Output ONLY valid JSON matching the schema below
- Do NOT add markdown fences, explanation, or prose outside the JSON
- If deadline cannot be extracted, set deadline to null
- Checklist items must be concrete and actionable (not vague like "study the material")

{{SCHEMA}}`

const weeklyPlanSystemPrompt = `You are a productive work-planning assistant. Given a list of active tasks/assignments with deadlines,
generate a realistic day-by-day plan for the current week.

Today is {{CURRENT_DATE}}. The user has approximately {{AVAILABLE_HOURS}} hours available per workday.

## Rules
- Distribute work realistically — do NOT overload any single day.
- Respect deadlines: tasks due sooner get scheduled first.
- Break large tasks (>3h) across multiple days when the deadline allows.
- Consider priority scores — higher priority items get prime time slots (morning).
- Include brief labels like "Morning", "Afternoon", or "Evening" for each block.
- If tasks exceed the available hours, note which ones may need rescheduling.

## Output
Output ONLY a raw JSON array — no markdown, no fences, no preamble.
Each day must match this shape:
[
  {
    "day": "Monday",
    "date": "YYYY-MM-DD",
    "blocks": [
      { "task": "Task title", "hours": 2, "slot": "Morning" }
    ],
    "totalHours": 5,
    "note": "Optional note about the day's load"
  }
]

Include only weekdays Mon–Fri. Skip today if it's past available hours.`

const extractTaskSystemPrompt = `You are a task extraction assistant. Parse the user's natural language input and extract
structured task data.

Today's date: {{CURRENT_DATE}}

Output ONLY a raw JSON object — no markdown, no fences, no preamble.
{
  "title": "string",
  "type": "Essay | Coding | Math | Research | Other",
  "deadline": "YYYY-MM-DD or null",
  "estimatedHours": number,
  "weight": "string or null (e.g. '20% of grade')",
  "notes": "string or null (extra context)"
}

Rules:
- Infer a concise, descriptive title from the text.
- If no deadline is mentioned, set deadline to null.
- If no time estimate, set estimatedHours to null.
- Be smart about parsing: "due Friday" means this coming Friday, "3 hours" means estimatedHours: 3.
- If the input is too vague to extract meaningful data, return { "error": "true", "reason": "..." }.`

const wbsSystemPrompt = `You are a professional project planner. Given a client brief or feature request,
decompose the work into concrete, actionable sub-tasks.

Output ONLY a raw JSON array — no markdown, no fences, no preamble.
Each item must match this shape exactly:
[
  {
    "title": "string",
    "estimatedHours": number,
    "impactScore": integer (1-10)
  }
]

Rules:
- 3 to 8 sub-tasks maximum
- Titles must be specific and actionable (not "Research" — use "Research X API authentication options")
- estimatedHours: realistic for a competent professional (not a beginner, not a hero)
- impactScore: how much this sub-task moves the needle on the overall deliverable (1=minor, 10=critical path)
- Today's date: {{CURRENT_DATE}}`

// render replaces {{NAME}} placeholders.
func render(tmpl string, vars map[string]string) string {
	if len(vars) == 0 {
		return tmpl
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}

// AnalyzeInput is everything the analysis prompt needs.
type AnalyzeInput struct {
	RawContent      string
	Profile         model.Profile
	BoostReason     string
	ExistingContext *model.Item
}

// BuildAnalyzePrompt returns the rendered system and user messages plus the
// feature tag used by the web app.
func BuildAnalyzePrompt(in AnalyzeInput, now time.Time) (system, user, feature string) {
	skills := strings.TrimSpace(in.Profile.Skills)
	if skills == "" {
		skills = "Not specified"
	}
	custom := strings.TrimSpace(in.Profile.CustomPriorityRule)
	if custom == "" {
		custom = "None"
	}
	preset := model.CanonicalPreset(in.Profile.PriorityPreset)

	system = render(analyzeSystemPrompt, map[string]string{
		"CURRENT_DATE": now.Format(model.DateLayout),
		"SKILLS":       skills,
		"PRESET":       preset,
		"CUSTOM_RULE":  custom,
		"SCHEMA":       JSONSchemaPrompt,
	})
	system = strings.TrimSpace(system)

	user = strings.TrimSpace(`Analyze this assignment. Think through the subtasks mentally first to ground your
time estimate, then output the JSON.

Assignment text:
"""
` + in.RawContent + `
"""`)

	feature = "priority_score"
	if strings.TrimSpace(in.BoostReason) != "" && in.ExistingContext != nil {
		existing, _ := json.Marshal(in.ExistingContext)
		user = "I am applying a boost to this previously analyzed assignment. The boost reason is: \"" +
			in.BoostReason + "\". Existing data: " + string(existing) +
			". Recalculate priorityScore to reflect this boost, and update priorityReasoning. Return the complete updated JSON schema."
		feature = "re_analyze"
	}
	return system, user, feature
}

// BuildWeeklyPlanPrompt builds the weekly plan messages.
func BuildWeeklyPlanPrompt(profile model.Profile, items []model.Item, now time.Time) (system, user string) {
	hours := profile.AvailableHoursPerDay
	if hours <= 0 {
		hours = model.DefaultAvailableHours
	}
	system = render(weeklyPlanSystemPrompt, map[string]string{
		"CURRENT_DATE":    now.Format(model.DateLayout),
		"AVAILABLE_HOURS": fmt.Sprintf("%d", hours),
	})
	system = strings.TrimSpace(system)

	lines := make([]string, 0, len(items))
	for _, it := range items {
		kind := it.Type
		if kind == "" {
			kind = "task"
		}
		deadline := "none"
		if it.Deadline != nil && *it.Deadline != "" {
			deadline = *it.Deadline
		}
		lines = append(lines, fmt.Sprintf(`- "%s" (type: %s, deadline: %s, est: %sh, priority: %d, status: %s)`,
			it.Title, kind, deadline, trimFloat(it.EstimatedHours), it.EffectiveScore(), it.Status))
	}
	summary := strings.Join(lines, "\n")
	if summary == "" {
		summary = "(No active tasks)"
	}
	user = "Active tasks:\n" + summary
	return system, user
}

// BuildExtractTaskPrompt builds the natural-language extraction messages.
func BuildExtractTaskPrompt(text string, now time.Time) (system, user string) {
	system = render(extractTaskSystemPrompt, map[string]string{
		"CURRENT_DATE": now.Format(model.DateLayout),
	})
	return strings.TrimSpace(system), text
}

// BuildWBSPrompt builds the work-breakdown messages.
func BuildWBSPrompt(brief string, now time.Time) (system, user string) {
	system = render(wbsSystemPrompt, map[string]string{
		"CURRENT_DATE": now.Format(model.DateLayout),
	})
	user = "Client brief / feature request:\n\"\"\"\n" + brief + "\n\"\"\""
	return strings.TrimSpace(system), user
}

func trimFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}
