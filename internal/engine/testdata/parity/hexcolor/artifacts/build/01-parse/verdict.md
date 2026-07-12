=== JUDGE VERDICT (dual-judge, 2 models) ===

AGGREGATE
req:   10.0
sec:   10.0
maint: 10.0
avg:   10.0
verdict: PASS
tests: GREEN  (go test -race ./...)

--- deepseek-v4-pro ---
req:10.0 sec:10.0 maint:10.0 avg:10.0 → PASS
Notes: The implementation perfectly matches the specification: it parses both #RGB (expanding correctly) and #RRGGBB, rejects all invalid inputs with wrapped ErrInvalid errors, and includes thorough table-driven tests. Code is clean, idiomatic, and secure—no vulnerabilities or missed edge cases.

--- gemma4-31b ---
req:10.0 sec:10.0 maint:10.0 avg:10.0 → PASS
Notes: The implementation perfectly adheres to the specification, including the subtle requirement of wrapping the sentinel error. The logic for expanding 3-digit hex codes (multiplying by 17 is a clever, correct shorthand for `x*16 + x`) is efficient, and the table-driven tests are comprehensive.
