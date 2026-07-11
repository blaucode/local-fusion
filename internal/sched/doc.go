// Package sched is the in-server cron loop for automations (model discover/eval
// proposals, lessons distillation). Everything it produces is proposal +
// human approval, never a silent write. P2 — graduates on demand
// (product-docs/ADOPTION.md §5). Not built in v2.0 core.
package sched
