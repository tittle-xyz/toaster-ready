// SPDX-License-Identifier: Apache-2.0

// Package render turns a scorecard into human-readable formats — Markdown (for
// PR comments / job summaries) and a plain HTML page — alongside the machine
// JSON. Both share one row/blocker builder so the views stay consistent. Output
// selection is intentionally simple for now; richer configuration can come later.
package render

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/tittle-xyz/toaster-ready/internal/scorecard"
)

type row struct {
	title, score, contribution, notes string
	weight                            float64
}

type blocker struct {
	title   string
	reasons []string
}

func rows(sc scorecard.Scorecard) []row {
	out := make([]row, 0, len(sc.Categories))
	for _, c := range sc.Categories {
		r := row{title: c.Title, weight: c.Weight}
		if !c.Applicable {
			r.score, r.contribution, r.notes = "—", "—", "n/a"
		} else {
			r.score = fmt.Sprintf("%.2f", c.Normalized)
			r.contribution = fmt.Sprintf("%.1f", c.Contribution)
			if !c.DataComplete {
				r.notes = "partial (no-data)"
			}
		}
		out = append(out, r)
	}
	return out
}

func blockers(sc scorecard.Scorecard) []blocker {
	var out []blocker
	for _, c := range sc.Categories {
		if len(c.BlockedBy) > 0 {
			out = append(out, blocker{title: c.Title, reasons: c.BlockedBy})
		}
	}
	return out
}

type rec struct{ title, action, cause string }

func recommendations(sc scorecard.Scorecard) []rec {
	var out []rec
	for _, c := range sc.Categories {
		for _, r := range c.Recommendations {
			out = append(out, rec{title: c.Title, action: r.Action, cause: string(r.Cause)})
		}
	}
	return out
}

func formatWeight(w float64) string { return strconv.FormatFloat(w, 'f', -1, 64) }

// Markdown renders the scorecard as a Markdown document.
func Markdown(sc scorecard.Scorecard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# toaster-ready scorecard — %s\n\n", sc.Repo)
	fmt.Fprintf(&b, "**Score: %.1f / %.0f** · band: **%s** · data complete: %v\n\n", sc.Score, sc.Max, sc.Band, sc.DataComplete)
	if len(sc.DetectedStack) > 0 {
		fmt.Fprintf(&b, "Detected stack: %s\n\n", strings.Join(sc.DetectedStack, ", "))
	}
	b.WriteString("| Category | Weight | Score | Contribution | Notes |\n")
	b.WriteString("|---|--:|--:|--:|---|\n")
	for _, r := range rows(sc) {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", r.title, formatWeight(r.weight), r.score, r.contribution, r.notes)
	}
	if recs := recommendations(sc); len(recs) > 0 {
		b.WriteString("\n## Recommendations\n")
		for _, r := range recs {
			fmt.Fprintf(&b, "- **%s** — %s _(%s)_\n", r.title, r.action, r.cause)
		}
	}
	if bl := blockers(sc); len(bl) > 0 {
		b.WriteString("\n## Incomplete / blocked\n")
		for _, x := range bl {
			for _, reason := range x.reasons {
				fmt.Fprintf(&b, "- **%s** — %s\n", x.title, reason)
			}
		}
	}
	fmt.Fprintf(&b, "\n_Scored by %s at %s._\n", sc.Scorer, sc.ScoredAt)
	return b.String()
}

const htmlStyle = `body{font:14px/1.5 system-ui,-apple-system,sans-serif;max-width:820px;margin:2rem auto;padding:0 1rem;color:#222}` +
	`table{border-collapse:collapse;width:100%}th,td{border:1px solid #ddd;padding:.4rem .6rem;text-align:left}` +
	`td.n{text-align:right}th{background:#f5f5f5}.score{font-size:1.2rem;font-weight:600}`

// HTML renders the scorecard as a plain, self-contained HTML page.
func HTML(sc scorecard.Scorecard) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">\n")
	fmt.Fprintf(&b, "<title>toaster-ready scorecard — %s</title>\n", esc(sc.Repo))
	fmt.Fprintf(&b, "<style>%s</style>\n</head><body>\n", htmlStyle)
	fmt.Fprintf(&b, "<h1>toaster-ready scorecard — %s</h1>\n", esc(sc.Repo))
	fmt.Fprintf(&b, "<p><span class=\"score\">Score: %.1f / %.0f</span> · band: <strong>%s</strong> · data complete: %v</p>\n",
		sc.Score, sc.Max, esc(sc.Band), sc.DataComplete)
	if len(sc.DetectedStack) > 0 {
		fmt.Fprintf(&b, "<p>Detected stack: %s</p>\n", esc(strings.Join(sc.DetectedStack, ", ")))
	}
	b.WriteString("<table>\n<thead><tr><th>Category</th><th>Weight</th><th>Score</th><th>Contribution</th><th>Notes</th></tr></thead>\n<tbody>\n")
	for _, r := range rows(sc) {
		fmt.Fprintf(&b, "<tr><td>%s</td><td class=\"n\">%s</td><td class=\"n\">%s</td><td class=\"n\">%s</td><td>%s</td></tr>\n",
			esc(r.title), formatWeight(r.weight), r.score, r.contribution, esc(r.notes))
	}
	b.WriteString("</tbody>\n</table>\n")
	if recs := recommendations(sc); len(recs) > 0 {
		b.WriteString("<h2>Recommendations</h2>\n<ul>\n")
		for _, r := range recs {
			fmt.Fprintf(&b, "<li><strong>%s</strong> — %s <em>(%s)</em></li>\n", esc(r.title), esc(r.action), esc(r.cause))
		}
		b.WriteString("</ul>\n")
	}
	if bl := blockers(sc); len(bl) > 0 {
		b.WriteString("<h2>Incomplete / blocked</h2>\n<ul>\n")
		for _, x := range bl {
			for _, reason := range x.reasons {
				fmt.Fprintf(&b, "<li><strong>%s</strong> — %s</li>\n", esc(x.title), esc(reason))
			}
		}
		b.WriteString("</ul>\n")
	}
	fmt.Fprintf(&b, "<p><small>Scored by %s at %s.</small></p>\n", esc(sc.Scorer), esc(sc.ScoredAt))
	b.WriteString("</body></html>\n")
	return b.String()
}
