// Ver 2026-09-15, by Opus 5

// Golden VM structures for TestGoldenVMStructure (viewmodel_golden_test.go):
// the full MacroReportVM JSON for the compact golden fixture, one constant
// per language. Regenerate after an INTENTIONAL VM shape or wording change
// with UPDATE_VM_GOLDEN=<dir> and paste the two files here — every other
// mismatch is a regression to investigate, not to re-record. The "+"+"`"("+")
// splices stand for the Markdown backtick-code marks inside the strings.
package report

const (
	goldenVMEN = `{
  "Title": "VMR Usage Report",
  "Meta": [
    {
      "Text": "Data source: 2 files · format 11 · 7 records (1 bad rows) · 2026-07-22 18:39:00 – 2026-07-24 02:00:00\n\n"
    },
    {
      "Text": "Config: /etc/vmr/report.yaml\n\n"
    },
    {
      "Summary": "File list",
      "Body": "a.jsonl, b.jsonl\n"
    },
    {
      "Text": "Details in [vmr-requests.md](./vmr-requests.md) · matching .json\n\n"
    },
    {
      "Text": "Task narratives in [journeys/index.md](journeys/index.md) (2 task(s) indexed · covers 2026-07-23 02:39:00 – 2026-07-24 10:00:00)\n\n"
    }
  ],
  "Sections": [
    {
      "ID": "summary",
      "Title": "§0 Summary",
      "Blocks": [
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Requests",
            "Success Rate",
            "Billed Input (fresh)⭐",
            "Cache Efficiency⭐",
            "p95 Duration",
            "PAYG-Equivalent Cost⭐"
          ],
          "Rows": [
            [
              "50 (fallback 3 / trunc 1)",
              "90.0%",
              "150.0K",
              "72.0%",
              "9.0s (n=44)",
              "unpriced"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "of which the interactive workload accounts for 10.0% (5/50)\n"
        },
        {
          "Text": "> ⭐ = derived/estimated metric (not direct upstream value), see Appendix for basis.\n\n"
        },
        {
          "Text": "**Highlights (auto):**\n- ⚠️ **heartbeat workload cache efficiency 11.1%** - 80.0K fresh tokens (dominates this workload's input)\n- ⚠️ **Endpoint openai-completions:prov1:gpt-x error rate 6.7/100** (worst), top cause rate_limit ×2\n\n"
        }
      ]
    },
    {
      "ID": "tokens",
      "Title": "§1 Cost & Token Economy",
      "Blocks": [
        {
          "Text": "**Token Class Breakdown** (basis: 45 records with usage)\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Category",
            "Amount",
            "Share"
          ],
          "Rows": [
            [
              "Input - cache hit",
              "350.0K",
              "70.0% of in"
            ],
            [
              "Input - fresh ⭐",
              "150.0K",
              "30.0% of in"
            ],
            [
              "Input - cache_write",
              "0",
              "-"
            ],
            [
              "Output",
              "120.0K",
              "-"
            ]
          ],
          "Notes": [
            "> Billing basis: fresh + cache_write(×premium) + out. Cache hits are billed free/near-free by most providers.\n> The figures in this section are a PAY-AS-YOU-GO EQUIVALENT: what this traffic would cost billed per token at the published prices vmr can resolve for it — the serving platform's own rate where one exists, otherwise the model maker's list price. They are not what you paid — a subscription/plan account's marginal cost is 0, and only you know the real unit price behind a reseller or proxy. They answer \"was this plan/proxy worth it\". Prices come from the standard table (generated 2026-08-01) plus any config.yaml account overrides (currency USD), and do not represent the prices in effect when these requests historically occurred; configure providers[].pricing.rates on the relevant provider to price at what you actually pay. See §2 Cost Estimate for details.\n\n"
          ]
        },
        {
          "Title": "**Cache Efficiency by Model** ⭐",
          "Fold": "",
          "Headers": [
            "Model",
            "Protocol",
            "Requests",
            "Cache Efficiency⭐",
            "fresh",
            "cached",
            "out"
          ],
          "Rows": [
            [
              "coding",
              "openai-completions",
              "50",
              "72.0%",
              "150.0K",
              "350.0K",
              "120.0K"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**Request Message Characters, Estimated Tokens & Share**",
          "Fold": "",
          "Headers": [
            "Role",
            "Chars",
            "Est. Tokens⭐",
            "Share⭐"
          ],
          "Rows": [
            [
              "tool",
              "140.0K",
              "35.0K",
              "70.0%"
            ],
            [
              "assistant",
              "48.0K",
              "12.0K",
              "24.0%"
            ],
            [
              "user",
              "12.0K",
              "3.0K",
              "6.0%"
            ]
          ],
          "Notes": [
            "> Est. Tokens⭐: upstream usage isn't broken down by role, so this is an estimate; share is computed on estimated tokens.\n> takeaway: when tool results dominate, the first lever for context optimization is compressing tool output, not the system prompt.\n\n"
          ]
        }
      ]
    },
    {
      "ID": "cost",
      "Title": "§2 Pay-As-You-Go Equivalent Cost",
      "Blocks": [
        {
          "Text": "> The figures in this section are a PAY-AS-YOU-GO EQUIVALENT: what this traffic would cost billed per token at the published prices vmr can resolve for it — the serving platform's own rate where one exists, otherwise the model maker's list price. They are not what you paid — a subscription/plan account's marginal cost is 0, and only you know the real unit price behind a reseller or proxy. They answer \"was this plan/proxy worth it\". Prices come from the standard table (generated 2026-08-01) plus any config.yaml account overrides (currency USD), and do not represent the prices in effect when these requests historically occurred; configure providers[].pricing.rates on the relevant provider to price at what you actually pay.\n\n"
        },
        {
          "Title": "**Estimated Cost by Date** (USD)",
          "Fold": "",
          "Headers": [
            "Date",
            "fresh",
            "out",
            "Est. Cost"
          ],
          "Rows": [
            [
              "2026-07-23",
              "150.0K",
              "120.0K",
              "3.2500 USD"
            ],
            [
              "**Total**",
              "",
              "",
              "3.2500 USD"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**Estimated Cost by Model** (USD)",
          "Fold": "",
          "Headers": [
            "Model",
            "Protocol",
            "fresh",
            "out",
            "Est. Cost"
          ],
          "Rows": [
            [
              "coding",
              "openai-completions",
              "150.0K",
              "120.0K",
              "3.2500 USD"
            ],
            [
              "**Total**",
              "",
              "",
              "",
              "3.2500 USD"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**Estimated Cost by Endpoint** (USD, merged across dates)",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "fresh",
            "out",
            "Est. Cost"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "100.0K",
              "80.0K",
              "3.2500 USD"
            ],
            [
              "**Total**",
              "",
              "",
              "3.2500 USD"
            ]
          ],
          "Notes": [
            "> The total excludes 1/2 endpoints with no priced traffic to attribute (the upstream endpoint is not in the price table, or the requests never reached one successfully) — cost unknown, not zero.\n\n"
          ]
        },
        {
          "Title": "**Estimated Cost by Client** (USD)",
          "Fold": "",
          "Headers": [
            "client_key",
            "fresh",
            "out",
            "Est. Cost"
          ],
          "Rows": [
            [
              "claw-a",
              "100.0K",
              "100.0K",
              "3.2500 USD"
            ],
            [
              "**Total**",
              "",
              "",
              "3.2500 USD"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "> Estimated cost includes requests where usage was not sniffed (priced via fallback estimation); the fresh/out columns only count confirmed token usage. Calculating unit price as \"Est. Cost ÷ Tokens\" may yield an inflated figure.\n\n"
        },
        {
          "Text": "<details><summary>Pricing sources used for this report</summary>\n\nstandard table generated 2026-08-01; 1 provider rate rule(s) applied\n</details>\n\n\n"
        }
      ]
    },
    {
      "ID": "providers",
      "Title": "§2.5 Provider Spend & Quota",
      "Blocks": [
        {
          "Text": "Cross-model roll-up by upstream account (config.yaml's providers[].name) — answers \"how much did this account consume overall, and how reliable was it\" without manually summing its endpoint rows.\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Provider",
            "Models",
            "Requests",
            "Success Rate",
            "fresh/cached/out",
            "Cache Eff.",
            "Dur. Mean",
            "Error Rate",
            "Top Error",
            "$ Estimate (USD)"
          ],
          "Rows": [
            [
              "prov1",
              "1",
              "28",
              "89.3%",
              "100.0K / 300.0K / 80.0K",
              "75.0%¹",
              "2.5s",
              "6.7%",
              "rate_limit 2(100.0%)",
              "3.2500"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "### Quota vs. Consumption\n\n"
        },
        {
          "Text": "Every account that declares a ` + "`" + `quota:` + "`" + `, with two independently-windowed consumption figures placed side by side — never subtracted or ratioed, each labeled with its own source.\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Provider",
            "Metric",
            "Window Consumed¹",
            "Used This Period²",
            "Amount",
            "Used%",
            "Elapsed%",
            "Period"
          ],
          "Rows": [
            [
              "prov1",
              "tokens",
              "123456",
              "69450000 (20.0% est.)",
              "50000000",
              "138.9%⭐",
              "20.5%",
              "07-24 ~ 07-29"
            ]
          ],
          "Notes": [
            "> ¹ Window Consumed: recomputed from this run's audit-log input — a RECOMPUTED figure, not a replay of the router's actual charge history. Accuracy differs per metric. **requests: no drift** — it reproduces the router's own ` + "`" + `multiplier × forwarded-attempt count` + "`" + ` formula literally (the router charges once per forwarded upstream success, failed attempts were never charged in the first place, and the multiplier is applied by exact multiplication with no rounding). **tokens** — requests whose upstream returned no exact usage are counted here with the same byte-count estimate the router charged (no longer counted as 0); the estimated share is shown as \"X% est.\" in parentheses. Both sides run the same formula; the one residual drift is that the router counts UPSTREAM bytes while this column can only count the bytes forwarded to the client, so the two differ by whatever response normalization rewrote (model-name rewrite, ` + "`" + `<think>` + "`" + ` stripping, ...). Common to both metrics: config weights/multipliers changed mid-window.\n",
            "> ² Used This Period: the router's own real-time counter from ` + "`" + `<log_dir>/vmr-quota.json` + "`" + ` — the authoritative account, in a different window than the column to its left. Never subtract or ratio the two. Shows ` + "`" + `-` + "`" + ` when the stored counter is still on an earlier period. The parenthesized \"X% est.\" marks how much of that consumption came from a degraded estimate (a byte-count fallback used when upstream didn't return exact usage), not authoritative metering.\n",
            "> ⭐ marks Used% >= 100%: this account is over its configured quota for the current period.\n",
            "\n"
          ]
        }
      ]
    },
    {
      "ID": "reliability",
      "Title": "§3 Reliability",
      "Blocks": [
        {
          "Text": "**Outcome Distribution**\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "ok",
            "error",
            "canceled",
            "truncated",
            "fallback(recovered/failed)⭐"
          ],
          "Rows": [
            [
              "45",
              "3",
              "1",
              "1",
              "3 (2/1)"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**Endpoint Health** (merged across dates)\n\n"
        },
        {
          "Text": "*openai-completions*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "Attempts",
            "OK",
            "Availability",
            "Error Rate⭐",
            "Top Error"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "30",
              "28",
              "93.3%",
              "6.7%",
              "rate_limit ×2"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "*anthropic-messages*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "Attempts",
            "OK",
            "Availability",
            "Error Rate⭐",
            "Top Error"
          ],
          "Rows": [
            [
              "anthropic-messages:prov2:claude-y",
              "4",
              "4",
              "100.0%",
              "0.0% ⚠️low-n",
              "-"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**Error Class × Endpoint** (non-zero only)\n\n"
        },
        {
          "Text": "*openai-completions*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "Class",
            "Count"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "rate_limit",
              "2(6.7%)"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**Quirk Fix × Endpoint** (non-zero only, % of this endpoint's successful attempts; see each request's detail page for the full narration)\n\n"
        },
        {
          "Text": "*anthropic-messages*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "Marker",
            "Count"
          ],
          "Rows": [
            [
              "anthropic-messages:prov2:claude-y",
              "think_strip",
              "2(50.0%)"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**Error Timeline** (errors / hour)\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"Errors / hour\"\n    x-axis [\"00\", \"01\", \"02\", \"03\", \"04\", \"05\", \"06\", \"07\", \"08\", \"09\", \"10\", \"11\", \"12\", \"13\", \"14\", \"15\", \"16\", \"17\", \"18\", \"19\", \"20\", \"21\", \"22\", \"23\"]\n    y-axis \"Errors\"\n    bar [0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]\n` + "`" + "`" + "`" + `\n\n"
        },
        {
          "Text": "> Errors peak at 09:00 (1 total).\n\n"
        }
      ]
    },
    {
      "ID": "latency",
      "Title": "§4 Latency & Throughput",
      "Blocks": [
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Model",
            "Protocol",
            "ttft p50/p95 (n)",
            "dur p50/p95/max (n)",
            "slow>30s⭐",
            "tok/s"
          ],
          "Rows": [
            [
              "coding",
              "openai-completions",
              "320ms / 950ms (n=44)",
              "1.5s / 9.0s / 35.0s (n=44)",
              "2",
              "100.5"
            ]
          ],
          "Notes": [
            "> Global p95 dur 9.0s, max 35.0s. Sorted by tok/s descending.\n> If coding's slowness mostly comes from a long stream rather than time-to-first-token, check the ttft vs dur gap per model.\n\n"
          ]
        },
        {
          "Text": "**By Endpoint** (merged across dates)\n\n"
        },
        {
          "Text": "*openai-completions*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "ttft p50/p95 (n)",
            "dur p50/p95/max (n)",
            "slow>30s⭐",
            "tok/s"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "300ms / 900ms (n=25)",
              "1.2s / 8.0s / 31.0s (n=25)",
              "2",
              "55.5"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "*anthropic-messages*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "ttft p50/p95 (n)",
            "dur p50/p95/max (n)",
            "slow>30s⭐",
            "tok/s"
          ],
          "Rows": [
            [
              "anthropic-messages:prov2:claude-y",
              "- (n=0)",
              "2.0s / 3.0s (n=4 ⚠️low-n)",
              "0",
              "40.1"
            ]
          ],
          "Notes": null
        }
      ]
    },
    {
      "ID": "workload",
      "Title": "§5 Workload Distribution",
      "Blocks": [
        {
          "Title": "**By Virtual Model**",
          "Fold": "",
          "Headers": [
            "Model",
            "Protocol",
            "Requests",
            "Success Rate",
            "fresh/cached/out",
            "dur p50/p95"
          ],
          "Rows": [
            [
              "coding",
              "openai-completions",
              "50",
              "90.0%",
              "150.0K / 350.0K / 120.0K",
              "1.5s/9.0s"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**By Workload Class**",
          "Fold": "",
          "Headers": [
            "Class",
            "Requests",
            "fresh",
            "Cache Efficiency⭐",
            "tool_call_rate",
            "dur p50/p95"
          ],
          "Rows": [
            [
              "interactive",
              "5",
              "50.0K",
              "75.0%",
              "40.0%",
              "-/-"
            ],
            [
              "heartbeat",
              "2",
              "80.0K",
              "11.1% ⚠️",
              "0.0%",
              "-/-"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**Hourly Activity**\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"Requests / hour\"\n    x-axis [\"00\", \"01\", \"02\", \"03\", \"04\", \"05\", \"06\", \"07\", \"08\", \"09\", \"10\", \"11\", \"12\", \"13\", \"14\", \"15\", \"16\", \"17\", \"18\", \"19\", \"20\", \"21\", \"22\", \"23\"]\n    y-axis \"Requests\"\n    bar [0, 0, 0, 0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0]\n` + "`" + "`" + "`" + `\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"Input Tokens / hour\"\n    x-axis [\"00\", \"01\", \"02\", \"03\", \"04\", \"05\", \"06\", \"07\", \"08\", \"09\", \"10\", \"11\", \"12\", \"13\", \"14\", \"15\", \"16\", \"17\", \"18\", \"19\", \"20\", \"21\", \"22\", \"23\"]\n    y-axis \"Token (M)\"\n    bar [0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.09, 0.00, 0.00, 0.00, 0.00, 0.03, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00]\n` + "`" + "`" + "`" + `\n\n"
        },
        {
          "Text": "**Daily Activity**\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"Requests / day\"\n    x-axis [\"07-23\"]\n    y-axis \"Requests\"\n    bar [50]\n` + "`" + "`" + "`" + `\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"Input Tokens / day\"\n    x-axis [\"07-23\"]\n    y-axis \"Token (M)\"\n    bar [0.50]\n` + "`" + "`" + "`" + `\n\n"
        },
        {
          "Title": "",
          "Fold": "<details><summary>+ Daily Activity Table (1 days)</summary>\n\n",
          "Headers": [
            "Date",
            "Requests",
            "Success Rate",
            "fresh/cached/out"
          ],
          "Rows": [
            [
              "2026-07-23",
              "50",
              "90.0%",
              "150.0K / 350.0K / 120.0K"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**By Client** ⭐",
          "Fold": "",
          "Headers": [
            "client_key",
            "Requests",
            "Success Rate",
            "fresh/cached/out(reasoning)",
            "Cache Eff.",
            "dur p50/p95",
            "In(p50/p95)",
            "Out(p50/p95)"
          ],
          "Rows": [
            [
              "claw-a",
              "7",
              "100.0%",
              "100.0K / 200.0K / 100.0K (0)",
              "0.0%",
              "-/-",
              "9.0K/31.0K",
              "2.0K/9.1K"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**By Endpoint** ⭐ (merged across dates)",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "Requests",
            "Success Rate",
            "fresh/cached/out(reasoning)",
            "Cache Eff.",
            "dur p50/p95",
            "In(p50/p95)",
            "Out(p50/p95)"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "28",
              "89.3%",
              "100.0K / 300.0K / 80.0K (0)",
              "75.0%¹",
              "1.2s/8.0s",
              "9.0K/30.0K",
              "2.0K/9.0K"
            ],
            [
              "anthropic-messages:prov2:claude-y",
              "4",
              "100.0%",
              "30.0K / 10.0K / 8.0K (0)",
              "25.0%",
              "2.0s/3.0s",
              "0/0",
              "0/0"
            ]
          ],
          "Notes": null
        }
      ]
    },
    {
      "ID": "client-endpoint",
      "Title": "§5.5 Per-Client Upstream Attribution",
      "Blocks": [
        {
          "Text": "Which upstream endpoints each client actually hit, and how many tokens landed on each — answers \"where does this agent's traffic actually land\".\n\n"
        },
        {
          "Title": "**claw-a**",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "Requests",
            "fresh",
            "cached",
            "out",
            "% of client's tokens"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "10",
              "50.0K",
              "150.0K",
              "30.0K",
              "100.0%"
            ]
          ],
          "Notes": null
        }
      ]
    },
    {
      "ID": "sessions",
      "Title": "§6 Sessions & Tasks",
      "Blocks": [
        {
          "Text": "> Session labels like s01 (l-...): sNN is a report-local row alias; l-<hash8> is the stable content-addressed ID.\n\n"
        },
        {
          "Text": "**claw-a**\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Session",
            "Time Range",
            "Title",
            "Turns",
            "Tasks",
            "fresh/cached/out",
            "Outcome"
          ],
          "Rows": [
            [
              "s01 (l-a00000000)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s02 (l-a00000001)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s03 (l-a00000002)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s04 (l-a00000003)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s05 (l-a00000004)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s06 (l-a00000005)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s07 (l-a00000006)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s08 (l-a00000007)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s09 (l-a00000008)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s10 (l-a00000009)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s11 (l-a00000010)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s12 (l-a00000011)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s13 (l-a00000012)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s14 (l-a00000013)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s15 (l-a00000014)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s16 (l-a00000015)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s17 (l-a00000016)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s18 (l-a00000017)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s19 (l-a00000018)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s20 (l-a00000019)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "",
          "Fold": "<details><summary>+ 2 more sessions (all ≤ 12 turns)</summary>\n\n",
          "Headers": [
            "Session",
            "Time Range",
            "Title",
            "Turns",
            "Tasks",
            "fresh/cached/out",
            "Outcome"
          ],
          "Rows": [
            [
              "s21 (l-a00000020)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s22 (l-a00000021)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**claw-b**\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Session",
            "Time Range",
            "Title",
            "Turns",
            "Tasks",
            "fresh/cached/out",
            "Outcome"
          ],
          "Rows": [
            [
              "l-d0",
              "07-24 01:00 → 01:03",
              "",
              "1",
              "1",
              "2.0K / 8.0K / 2.0K",
              "ok"
            ],
            [
              "[s23 (l-d1)](stories/j-claw-b-1.md)",
              "07-24 02:00 → 02:02",
              "follow-up &lt;!-- ok",
              "1",
              "1",
              "2.0K / 8.0K / 2.0K",
              "ok"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "> l-d1 ← l-d0 (single compaction)\n\n"
        }
      ]
    },
    {
      "ID": "sticky",
      "Title": "§6.5 Sticky Effectiveness ⭐",
      "Blocks": [
        {
          "Text": "Within the same session: requests that landed back on the **previous request's endpoint** vs. requests that **switched endpoints** — cache efficiency compared between the two groups.\nThe Sticky Model's only reason to exist is keeping the upstream prompt cache warm — this section is the evidence for whether it actually delivers that.\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Group",
            "Requests",
            "With usage",
            "Cache Efficiency⭐",
            "cached",
            "fresh"
          ],
          "Rows": [
            [
              "Same endpoint",
              "30",
              "28",
              "75.0%",
              "300.0K",
              "100.0K"
            ],
            [
              "Switched endpoint",
              "12",
              "10",
              "25.0%¹",
              "30.0K",
              "90.0K"
            ]
          ],
          "Notes": [
            "> Not enough samples (either group's usage-bearing record count < 20); no conclusion drawn this period.\n>\n> Basis: a session's first request (8) has no prior request to compare against and isn't counted in either group; 2 records that couldn't be grouped into a session are likewise excluded.\n> **Doesn't explain WHY a switch happened**: sticky_ttl expiry, endpoint cooldown, conditional routing eliminating the sticky pick, or the model simply not having sticky enabled — these can't be told apart after the fact; this section only states what happened.\n\n"
          ]
        }
      ]
    },
    {
      "ID": "endpoint-value",
      "Title": "§6.6 Endpoint Value ⭐",
      "Blocks": [
        {
          "Text": "Cost per unit of output delivered, not just total spend — a cheap-per-request endpoint that fails often can be more expensive once you account for the retry it forces.\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Endpoint",
            "Successful Requests",
            "out tokens",
            "Cost/1M out (USD)",
            "Cost/Success Req (USD)",
            "Failed Attempts",
            "Availability",
            "Wasted Time⭐"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "25",
              "80.0K",
              "40.6250",
              "0.1300",
              "2",
              "93.3%",
              "5.0s"
            ],
            [
              "anthropic-messages:prov2:claude-y",
              "4",
              "8.0K",
              "-",
              "-",
              "0",
              "100.0%",
              "-"
            ]
          ],
          "Notes": [
            "> Wasted Time⭐ = this endpoint's **failed attempts'** cumulative wall-clock time: the request was eventually completed elsewhere, so this time is pure latency loss.\n> **Time only, never converted to money**: failed attempts carry no usage (vmr only extracts it from the response the client actually received), and most providers don't bill failed requests anyway —\n> putting a dollar figure on it would be fabricated. The basis here is \"how much longer did it make you wait\", not \"how much did it cost you\".\n> Cost/1M out is for apples-to-apples comparison (which is cheaper for the same 1M tokens produced); cost/successful request is shaped by each endpoint's own request mix — check §5's workload profile before comparing across endpoints.\n\n"
          ]
        }
      ]
    },
    {
      "ID": "compactions",
      "Title": "§6.7 Compaction Reconstruction ⭐",
      "Blocks": [
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Time",
            "Summarized Session",
            "Continues To",
            "tokens_in → tokens_out",
            "Retention",
            "Swallowed Entities (sample)"
          ],
          "Rows": [
            [
              "2026-07-23 02:00:00",
              "l-abc12345",
              "l-def67890",
              "120.0K → 9.0K",
              "8.0%",
              "e1, e2, e3 (+1 more)"
            ]
          ],
          "Notes": [
            "> Retention = tokens_out / tokens_in; retention ≥ 100% means output did not shrink (may indicate detector false-positive or structured expansion).\n\n"
          ]
        }
      ]
    },
    {
      "ID": "efficiency",
      "Title": "§7 Efficiency & Waste ⭐",
      "Blocks": [
        {
          "Text": "> **Total shipped** 48.0 KB · **Dead weight** 24.0 KB (50%) · **≈ tokens wasted** 6.0K · **Tool-set shapes** 1\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Finding",
            "Metric",
            "Value",
            "Implicated",
            "Action"
          ],
          "Rows": [
            [
              "Cache-missed input",
              "cache_miss_tokens",
              "150.0K (30.0%)",
              "Global, coding accounts for 150.0K",
              "Check prompt-prefix stability / enable provider caching"
            ],
            [
              "Scheduled-task redundancy",
              "fresh + cache_eff",
              "80.0K fresh, cache efficiency 11.1%",
              "heartbeat",
              "Lengthen the interval / switch to a cheaper model / cache the prefix"
            ],
            [
              "Output truncation",
              "truncated",
              "1/50",
              "stream interrupted",
              "Investigate upstream timeouts / raise stream_idle"
            ],
            [
              "Slow requests",
              "slow_request_share",
              "~5% > 30s",
              "see §4 stream_ms attribution",
              "see §4"
            ],
            [
              "Quota nearing exhaustion",
              "provider_quota_used_pct",
              "138.9% (tokens · )",
              "prov1",
              "Review this account's or model's routing weight or quota configuration"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**Tool Shape Waste Top-5** (sorted by wasted bytes descending; full detail in vmr-report.json -> tools[])\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "Shape",
            "Requests",
            "Declared",
            "Used",
            "Utilization",
            "Wasted Bytes"
          ],
          "Rows": [
            [
              "tools:4",
              "6",
              "4",
              "2",
              "50.0%",
              "24.0 KB"
            ]
          ],
          "Notes": [
            "<details><summary>tools:4 · 6 requests · 4 declared · 2 actually called</summary>\n\n**Tools called (2, by call count descending):**\n\n1. read (5×)\n2. exec (2×)\n\n**Declared but never called (2, alphabetical):**\n\n1. search\n2. write\n\n</details>\n\n",
            "> Stats window = this report's input log range; low-frequency tools (e.g. cron-triggered ones) may fall outside it — base trimming decisions on ≥1 week of logs.\n\n"
          ]
        }
      ]
    },
    {
      "ID": "request-index",
      "Title": "§8 Request Detail Index",
      "Blocks": [
        {
          "Text": "Every record (Chat User -> Session -> Task -> Turn) is in [vmr-requests.md](./vmr-requests.md).\n\n"
        },
        {
          "Text": "This run did not write ` + "`" + `details/*.md` + "`" + ` (generated on demand by default). Fetch a single record any time by its coordinate (the \"File\" column of ` + "`" + `vmr-requests.md` + "`" + ` shows it as this coordinate when no details were generated, ` + "`" + `basename:line` + "`" + `): ` + "`" + `vmr replay -print -req <coord>` + "`" + `, e.g. ` + "`" + `vmr replay -print -req a.jsonl:3` + "`" + `; or pass ` + "`" + `-details` + "`" + ` to materialize all of them.\n\n"
        }
      ]
    },
    {
      "ID": "appendix",
      "Title": "Appendix: Data Source & Methodology",
      "Blocks": null
    }
  ],
  "Disclaimers": [
    "- Input: a.jsonl, b.jsonl · format 11 · 7 records / 1 bad rows\n",
    "- Period: 2026-07-22 18:39:00 – 2026-07-24 02:00:00 (local timezone)\n",
    "- Percentile method: nearest-rank\n",
    "- n basis: each percentile is annotated with n (= ttft_known / requests_with_dur / stream_known); n<20 is marked ⚠️low-n.\n",
    "- Ratio low confidence: cache_efficiency and similar ratio metrics get a ¹ footnote when their denominator / total requests < 90%.\n",
    "- ⭐ marker: this column is a derived/estimated metric (not a value returned directly by the upstream) — read it together with its sample size and basis note.\n",
    "- Billing basis: fresh + cache_write(premium) + out; cache hits are billed free/near-free by most providers. \n",
    "- Slow-request threshold: 30s\n"
  ],
  "Footnotes": [
    {
      "ID": "self-traffic",
      "Text": "- Self-traffic: exclusion active; 2 analysis request(s) from ` + "`" + `vmr story -llm-addr` + "`" + ` itself removed from every total (disable with ` + "`" + `-include-self-traffic` + "`" + `).\n"
    },
    {
      "ID": "client-reconciliation",
      "Text": "- Client reconciliation: clients present in cost/workload tables without a standalone sibling file (single-shot scheduled traffic only, rolled into cron files): claw-a.\n"
    }
  ]
}
`
	goldenVMZH = `{
  "Title": "VMR 用量报告",
  "Meta": [
    {
      "Text": "数据源: 2 个文件 · format 11 · 7 条记录（1 坏行）· 2026-07-22 18:39:00 – 2026-07-24 02:00:00\n\n"
    },
    {
      "Text": "配置: /etc/vmr/report.yaml\n\n"
    },
    {
      "Summary": "文件清单",
      "Body": "a.jsonl, b.jsonl\n"
    },
    {
      "Text": "详单见 [vmr-requests.md](./vmr-requests.md) · 同名 .json\n\n"
    },
    {
      "Text": "任务叙事见 [journeys/index.md](journeys/index.md)（2 个任务索引 · 覆盖 2026-07-23 02:39:00 – 2026-07-24 10:00:00）\n\n"
    }
  ],
  "Sections": [
    {
      "ID": "summary",
      "Title": "§0 摘要",
      "Blocks": [
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "请求",
            "成功率",
            "计费输入(fresh)⭐",
            "缓存效率⭐",
            "p95 耗时",
            "按量计费等价成本⭐"
          ],
          "Rows": [
            [
              "50（fallback 3 / trunc 1）",
              "90.0%",
              "150.0K",
              "72.0%",
              "9.0s (n=44)",
              "未定价"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "其中 interactive 工作负载占 10.0%（5/50）\n"
        },
        {
          "Text": "> ⭐ = 衍生/预估指标（非上游直接返回值），完整口径见附录。\n\n"
        },
        {
          "Text": "**亮点 (auto):**\n- ⚠️ **heartbeat 工作负载缓存效率 11.1%** - 80.0K fresh tokens（占该负载输入大头）\n- ⚠️ **端点 openai-completions:prov1:gpt-x 错误率 6.7/100**（最差），主因 rate_limit ×2\n\n"
        }
      ]
    },
    {
      "ID": "tokens",
      "Title": "§1 成本与 Token 经济",
      "Blocks": [
        {
          "Text": "**Token 类别分解**（basis: 45 条带 usage 的记录）\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "类别",
            "数量",
            "占比"
          ],
          "Rows": [
            [
              "输入-缓存命中",
              "350.0K",
              "70.0% of in"
            ],
            [
              "输入-fresh ⭐",
              "150.0K",
              "30.0% of in"
            ],
            [
              "输入-cache_write",
              "0",
              "-"
            ],
            [
              "输出",
              "120.0K",
              "-"
            ]
          ],
          "Notes": [
            "> 计费口径：fresh + cache_write(×溢价) + out。缓存命中按各厂免费/极低价计。\n> 本章金额是**按量计费等价成本**：这些流量若按 vmr 能解析到的公开价逐 Token 计费要花多少——渠道有自定价时用渠道价，否则用第一方列表价。它不是实付金额——包月/套餐账号的边际成本是 0，经转售商或代理的实际单价也只有你自己知道。它回答的是「这个套餐/代理买得值不值」。价格取自标准价目表（生成于 2026-08-01）与 config.yaml 的账号覆盖（货币 USD），不代表历史请求实际发生时的价格；要用实付价请在对应 provider 的 providers[].pricing.rates 里写明。 详见 §2 成本估算。\n\n"
          ]
        },
        {
          "Title": "**按模型缓存效率** ⭐",
          "Fold": "",
          "Headers": [
            "模型",
            "协议",
            "请求",
            "缓存效率⭐",
            "fresh",
            "cached",
            "out"
          ],
          "Rows": [
            [
              "coding",
              "openai-completions",
              "50",
              "72.0%",
              "150.0K",
              "350.0K",
              "120.0K"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**请求消息字符、预估Token及占比**",
          "Fold": "",
          "Headers": [
            "角色",
            "字符",
            "预估Token⭐",
            "占比⭐"
          ],
          "Rows": [
            [
              "tool",
              "140.0K",
              "35.0K",
              "70.0%"
            ],
            [
              "assistant",
              "48.0K",
              "12.0K",
              "24.0%"
            ],
            [
              "user",
              "12.0K",
              "3.0K",
              "6.0%"
            ]
          ],
          "Notes": [
            "> 预估Token⭐：上游 usage 不按角色拆分，无法拿到真实值，这里使用估算公式计算；占比按预估Token 计算。\n> takeaway: tool 结果占比最大时，上下文优化的首要杠杆是压缩 tool 返回，而非 system prompt。\n\n"
          ]
        }
      ]
    },
    {
      "ID": "cost",
      "Title": "§2 按量计费等价成本",
      "Blocks": [
        {
          "Text": "> 本章金额是**按量计费等价成本**：这些流量若按 vmr 能解析到的公开价逐 Token 计费要花多少——渠道有自定价时用渠道价，否则用第一方列表价。它不是实付金额——包月/套餐账号的边际成本是 0，经转售商或代理的实际单价也只有你自己知道。它回答的是「这个套餐/代理买得值不值」。价格取自标准价目表（生成于 2026-08-01）与 config.yaml 的账号覆盖（货币 USD），不代表历史请求实际发生时的价格；要用实付价请在对应 provider 的 providers[].pricing.rates 里写明。\n\n"
        },
        {
          "Title": "**按日估算成本**（USD）",
          "Fold": "",
          "Headers": [
            "日期",
            "fresh",
            "out",
            "估算成本"
          ],
          "Rows": [
            [
              "2026-07-23",
              "150.0K",
              "120.0K",
              "3.2500 USD"
            ],
            [
              "**合计**",
              "",
              "",
              "3.2500 USD"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**按模型估算成本**（USD）",
          "Fold": "",
          "Headers": [
            "模型",
            "协议",
            "fresh",
            "out",
            "估算成本"
          ],
          "Rows": [
            [
              "coding",
              "openai-completions",
              "150.0K",
              "120.0K",
              "3.2500 USD"
            ],
            [
              "**合计**",
              "",
              "",
              "",
              "3.2500 USD"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**按端点估算成本**（USD，跨日合并）",
          "Fold": "",
          "Headers": [
            "端点",
            "fresh",
            "out",
            "估算成本"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "100.0K",
              "80.0K",
              "3.2500 USD"
            ],
            [
              "**合计**",
              "",
              "",
              "3.2500 USD"
            ]
          ],
          "Notes": [
            "> 合计不含 1/2 个端点——没有可归属的已定价流量（上游端点不在定价表内，或其请求未成功送达任何上游端点）。成本未知，不是 0。\n\n"
          ]
        },
        {
          "Title": "**按客户端估算成本**（USD）",
          "Fold": "",
          "Headers": [
            "client_key",
            "fresh",
            "out",
            "估算成本"
          ],
          "Rows": [
            [
              "claw-a",
              "100.0K",
              "100.0K",
              "3.2500 USD"
            ],
            [
              "**合计**",
              "",
              "",
              "3.2500 USD"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "> 估算成本包含了未嗅探到 usage 的请求（按降级估算定价计入）；而 fresh/out 列仅统计已确认的 Token 数量。若按「估算成本 ÷ Token」反推单价可能偏高。\n\n"
        },
        {
          "Text": "<details><summary>本次使用的定价来源</summary>\n\nstandard table generated 2026-08-01; 1 provider rate rule(s) applied\n</details>\n\n\n"
        }
      ]
    },
    {
      "ID": "providers",
      "Title": "§2.5 账户（Provider）消耗与额度",
      "Blocks": [
        {
          "Text": "按上游账户（config.yaml 的 providers[].name）上卷的跨模型汇总——回答\"这个账户整体消耗多少、可靠性如何\"，而不是逐个模型手动相加。\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "账户",
            "模型数",
            "请求",
            "成功率",
            "fresh/cached/out",
            "缓存效率",
            "均值耗时",
            "错误率",
            "主要错误类",
            "$ 估算 (USD)"
          ],
          "Rows": [
            [
              "prov1",
              "1",
              "28",
              "89.3%",
              "100.0K / 300.0K / 80.0K",
              "75.0%¹",
              "2.5s",
              "6.7%",
              "rate_limit 2(100.0%)",
              "3.2500"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "### 额度与消耗对照\n\n"
        },
        {
          "Text": "只列配了 ` + "`" + `quota:` + "`" + ` 的账户，把两个不同时间窗口的消耗数字并排给出——不做减法、不算覆盖率，各自标注来源。\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "账户",
            "metric",
            "本报表窗口消耗¹",
            "本周期已用²",
            "上限",
            "已用%",
            "周期已过%",
            "周期区间"
          ],
          "Rows": [
            [
              "prov1",
              "tokens",
              "123456",
              "69450000（20.0% 估算）",
              "50000000",
              "138.9%⭐",
              "20.5%",
              "07-24 ~ 07-29"
            ]
          ],
          "Notes": [
            "> ¹ 本报表窗口消耗：从本次输入的审计日志重算得到，是**重算值**，不是路由半区当时记账的重放。两种口径的精度不同：**requests 口径无出入**——按 ` + "`" + `倍率 × 已转发尝试数` + "`" + ` 逐字复现路由半区的记账公式（路由每转发一次上游成功响应记一次账，失败尝试本就不记，倍率精确相乘、不取整）；**tokens 口径**：上游未返回精确 usage 的请求，本列与路由半区一样按字节数估算计入（不再计 0），估算占比见括号内的\"X% 估算\"标注——两侧公式相同，唯一残留出入是路由半区数的是**上游原始字节**、本列只能数**转发给客户端的字节**，当响应正规化改写过内容（模型名改写、` + "`" + `<think>` + "`" + ` 剥离等）时两者会差出这段字节。两种口径共同的出入源：config 里的权重/倍率在本窗口期内被改过。\n",
            "> ² 本周期已用：来自 ` + "`" + `<log_dir>/vmr-quota.json` + "`" + ` 的实时计数器，是路由半区的权威记账——与上一列的统计窗口不同，两者不可相减、不可求比值。计数器仍停留在更早周期时显示 ` + "`" + `-` + "`" + `。括号内的\"X% 估算\"标注这段消耗里有多少来自降级估算（上游未返回精确 usage 时的字节数粗估），不是精确记账。\n",
            "> ⭐ 已用% ≥ 100% 时的标记：该账户本周期已超出配置的额度上限。\n",
            "\n"
          ]
        }
      ]
    },
    {
      "ID": "reliability",
      "Title": "§3 可靠性",
      "Blocks": [
        {
          "Text": "**结果分布**\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "ok",
            "error",
            "canceled",
            "truncated",
            "fallback(恢复/失败)⭐"
          ],
          "Rows": [
            [
              "45",
              "3",
              "1",
              "1",
              "3 (2/1)"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**端点健康**（跨日合并）\n\n"
        },
        {
          "Text": "*openai-completions*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "端点",
            "尝试",
            "成功",
            "可用度",
            "错误率⭐",
            "首要错误"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "30",
              "28",
              "93.3%",
              "6.7%",
              "rate_limit ×2"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "*anthropic-messages*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "端点",
            "尝试",
            "成功",
            "可用度",
            "错误率⭐",
            "首要错误"
          ],
          "Rows": [
            [
              "anthropic-messages:prov2:claude-y",
              "4",
              "4",
              "100.0%",
              "0.0% ⚠️low-n",
              "-"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**错误类别 × 端点**（仅非零）\n\n"
        },
        {
          "Text": "*openai-completions*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "端点",
            "类别",
            "计数"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "rate_limit",
              "2(6.7%)"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**Quirk 修复 × 端点**（仅非零，占该端点成功尝试的比例；详见每条请求的详情页）\n\n"
        },
        {
          "Text": "*anthropic-messages*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "端点",
            "标记",
            "计数"
          ],
          "Rows": [
            [
              "anthropic-messages:prov2:claude-y",
              "think_strip",
              "2(50.0%)"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**错误时间线**（错误数 / 小时）\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"错误数 / 小时\"\n    x-axis [\"00\", \"01\", \"02\", \"03\", \"04\", \"05\", \"06\", \"07\", \"08\", \"09\", \"10\", \"11\", \"12\", \"13\", \"14\", \"15\", \"16\", \"17\", \"18\", \"19\", \"20\", \"21\", \"22\", \"23\"]\n    y-axis \"错误数\"\n    bar [0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]\n` + "`" + "`" + "`" + `\n\n"
        },
        {
          "Text": "> 错误集中在 09:00（共 1 条）。\n\n"
        }
      ]
    },
    {
      "ID": "latency",
      "Title": "§4 延迟与吞吐",
      "Blocks": [
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "模型",
            "协议",
            "ttft p50/p95 (n)",
            "dur p50/p95/max (n)",
            "slow>30s⭐",
            "tok/s"
          ],
          "Rows": [
            [
              "coding",
              "openai-completions",
              "320ms / 950ms (n=44)",
              "1.5s / 9.0s / 35.0s (n=44)",
              "2",
              "100.5"
            ]
          ],
          "Notes": [
            "> 全局 p95 dur 9.0s，max 35.0s。按 tok/s 降序排列。\n> 若 coding 的慢主要来自长流式输出，而非首字延迟，参见每模型的 ttft vs dur 差值。\n\n"
          ]
        },
        {
          "Text": "**按端点**（跨日合并）\n\n"
        },
        {
          "Text": "*openai-completions*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "端点",
            "ttft p50/p95 (n)",
            "dur p50/p95/max (n)",
            "slow>30s⭐",
            "tok/s"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "300ms / 900ms (n=25)",
              "1.2s / 8.0s / 31.0s (n=25)",
              "2",
              "55.5"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "*anthropic-messages*\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "端点",
            "ttft p50/p95 (n)",
            "dur p50/p95/max (n)",
            "slow>30s⭐",
            "tok/s"
          ],
          "Rows": [
            [
              "anthropic-messages:prov2:claude-y",
              "- (n=0)",
              "2.0s / 3.0s (n=4 ⚠️low-n)",
              "0",
              "40.1"
            ]
          ],
          "Notes": null
        }
      ]
    },
    {
      "ID": "workload",
      "Title": "§5 负载分布",
      "Blocks": [
        {
          "Title": "**按虚拟模型**",
          "Fold": "",
          "Headers": [
            "模型",
            "协议",
            "请求",
            "成功率",
            "fresh/cached/out",
            "dur p50/p95"
          ],
          "Rows": [
            [
              "coding",
              "openai-completions",
              "50",
              "90.0%",
              "150.0K / 350.0K / 120.0K",
              "1.5s/9.0s"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**按工作负载类**",
          "Fold": "",
          "Headers": [
            "类",
            "请求",
            "fresh",
            "缓存效率⭐",
            "tool_call_rate",
            "dur p50/p95"
          ],
          "Rows": [
            [
              "interactive",
              "5",
              "50.0K",
              "75.0%",
              "40.0%",
              "-/-"
            ],
            [
              "heartbeat",
              "2",
              "80.0K",
              "11.1% ⚠️",
              "0.0%",
              "-/-"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**每小时活跃度**\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"请求量 / 小时\"\n    x-axis [\"00\", \"01\", \"02\", \"03\", \"04\", \"05\", \"06\", \"07\", \"08\", \"09\", \"10\", \"11\", \"12\", \"13\", \"14\", \"15\", \"16\", \"17\", \"18\", \"19\", \"20\", \"21\", \"22\", \"23\"]\n    y-axis \"请求\"\n    bar [0, 0, 0, 0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0]\n` + "`" + "`" + "`" + `\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"输入Token / 小时\"\n    x-axis [\"00\", \"01\", \"02\", \"03\", \"04\", \"05\", \"06\", \"07\", \"08\", \"09\", \"10\", \"11\", \"12\", \"13\", \"14\", \"15\", \"16\", \"17\", \"18\", \"19\", \"20\", \"21\", \"22\", \"23\"]\n    y-axis \"Token (M)\"\n    bar [0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.09, 0.00, 0.00, 0.00, 0.00, 0.03, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00, 0.00]\n` + "`" + "`" + "`" + `\n\n"
        },
        {
          "Text": "**按日期活跃度**\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"请求量 / 天\"\n    x-axis [\"07-23\"]\n    y-axis \"请求\"\n    bar [50]\n` + "`" + "`" + "`" + `\n\n` + "`" + "`" + "`" + `mermaid\nxychart-beta\n    title \"输入Token / 天\"\n    x-axis [\"07-23\"]\n    y-axis \"Token (M)\"\n    bar [0.50]\n` + "`" + "`" + "`" + `\n\n"
        },
        {
          "Title": "",
          "Fold": "<details><summary>+ 逐日活跃度明细表（共 1 天）</summary>\n\n",
          "Headers": [
            "日期",
            "请求",
            "成功率",
            "fresh/cached/out"
          ],
          "Rows": [
            [
              "2026-07-23",
              "50",
              "90.0%",
              "150.0K / 350.0K / 120.0K"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**按客户端** ⭐",
          "Fold": "",
          "Headers": [
            "client_key",
            "请求",
            "成功率",
            "fresh/cached/out(reasoning)",
            "缓存效率",
            "dur p50/p95",
            "In(p50/p95)",
            "Out(p50/p95)"
          ],
          "Rows": [
            [
              "claw-a",
              "7",
              "100.0%",
              "100.0K / 200.0K / 100.0K (0)",
              "0.0%",
              "-/-",
              "9.0K/31.0K",
              "2.0K/9.1K"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "**按端点** ⭐（跨日合并）",
          "Fold": "",
          "Headers": [
            "端点",
            "请求",
            "成功率",
            "fresh/cached/out(reasoning)",
            "缓存效率",
            "dur p50/p95",
            "In(p50/p95)",
            "Out(p50/p95)"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "28",
              "89.3%",
              "100.0K / 300.0K / 80.0K (0)",
              "75.0%¹",
              "1.2s/8.0s",
              "9.0K/30.0K",
              "2.0K/9.0K"
            ],
            [
              "anthropic-messages:prov2:claude-y",
              "4",
              "100.0%",
              "30.0K / 10.0K / 8.0K (0)",
              "25.0%",
              "2.0s/3.0s",
              "0/0",
              "0/0"
            ]
          ],
          "Notes": null
        }
      ]
    },
    {
      "ID": "client-endpoint",
      "Title": "§5.5 按客户端的上游归属",
      "Blocks": [
        {
          "Text": "每个客户端命中了哪些上游端点、各自拿走了多少 token——回答\"这个 Agent 的流量到底落到哪几个账户/模型上了\"。\n\n"
        },
        {
          "Title": "**claw-a**",
          "Fold": "",
          "Headers": [
            "端点",
            "请求",
            "fresh",
            "cached",
            "out",
            "占该客户端 token 的比例"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "10",
              "50.0K",
              "150.0K",
              "30.0K",
              "100.0%"
            ]
          ],
          "Notes": null
        }
      ]
    },
    {
      "ID": "sessions",
      "Title": "§6 会话与任务",
      "Blocks": [
        {
          "Text": "> 会话标识形如 s01 (l-...)：sNN 仅为本次报告内行号别名，括号内 l-<hash8> 为稳定内容寻址 ID。\n\n"
        },
        {
          "Text": "**claw-a**\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "会话",
            "时间范围",
            "标题",
            "轮",
            "任务",
            "fresh/cached/out",
            "结果"
          ],
          "Rows": [
            [
              "s01 (l-a00000000)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s02 (l-a00000001)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s03 (l-a00000002)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s04 (l-a00000003)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s05 (l-a00000004)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s06 (l-a00000005)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s07 (l-a00000006)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s08 (l-a00000007)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s09 (l-a00000008)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s10 (l-a00000009)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s11 (l-a00000010)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s12 (l-a00000011)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s13 (l-a00000012)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s14 (l-a00000013)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s15 (l-a00000014)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s16 (l-a00000015)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s17 (l-a00000016)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s18 (l-a00000017)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s19 (l-a00000018)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s20 (l-a00000019)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ]
          ],
          "Notes": null
        },
        {
          "Title": "",
          "Fold": "<details><summary>+ 其余 2 个会话（均 ≤ 12 轮）</summary>\n\n",
          "Headers": [
            "会话",
            "时间范围",
            "标题",
            "轮",
            "任务",
            "fresh/cached/out",
            "结果"
          ],
          "Rows": [
            [
              "s21 (l-a00000020)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ],
            [
              "s22 (l-a00000021)",
              "07-22 18:39 → 18:52",
              "morning routine",
              "3",
              "1",
              "10.0K / 30.0K / 10.0K",
              "ok"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**claw-b**\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "会话",
            "时间范围",
            "标题",
            "轮",
            "任务",
            "fresh/cached/out",
            "结果"
          ],
          "Rows": [
            [
              "l-d0",
              "07-24 01:00 → 01:03",
              "",
              "1",
              "1",
              "2.0K / 8.0K / 2.0K",
              "ok"
            ],
            [
              "[s23 (l-d1)](stories/j-claw-b-1.md)",
              "07-24 02:00 → 02:02",
              "follow-up &lt;!-- ok",
              "1",
              "1",
              "2.0K / 8.0K / 2.0K",
              "ok"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "> l-d1 ← l-d0（单次 compaction）\n\n"
        }
      ]
    },
    {
      "ID": "sticky",
      "Title": "§6.5 Sticky 有效性 ⭐",
      "Blocks": [
        {
          "Text": "同一会话内，落回**上一条请求所用端点**的请求 vs **换了端点**的请求，两组缓存效率对比。\nSticky Model（设计文档 §6.5）存在的唯一理由是让上游 prompt cache 保温——这一节是它到底有没有兑现的证据。\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "组",
            "请求",
            "带 usage",
            "缓存效率⭐",
            "cached",
            "fresh"
          ],
          "Rows": [
            [
              "落回同一端点",
              "30",
              "28",
              "75.0%",
              "300.0K",
              "100.0K"
            ],
            [
              "换了端点",
              "12",
              "10",
              "25.0%¹",
              "30.0K",
              "90.0K"
            ]
          ],
          "Notes": [
            "> 样本不足（任一组带 usage 的记录 < 20 条），本期不下结论。\n>\n> 口径：会话首条请求（8 条）没有前一条可比，不计入任何一组；未能归入会话的 2 条同样不计入。\n> **不解释切换原因**：sticky_ttl 到期、端点冷却、条件路由淘汰了 sticky 首选、该模型压根没开 sticky——事后无法区分，本节只陈述发生了什么。\n\n"
          ]
        }
      ]
    },
    {
      "ID": "endpoint-value",
      "Title": "§6.6 端点性价比 ⭐",
      "Blocks": [
        {
          "Text": "单位产出的代价，而不只是总花费——一个单价便宜但经常失败的端点，把请求推给下一家之后的真实代价可能更高。\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "端点",
            "成功请求",
            "out tokens",
            "成本/1M out (USD)",
            "成本/成功请求 (USD)",
            "失败尝试",
            "可用率",
            "失败耗时⭐"
          ],
          "Rows": [
            [
              "openai-completions:prov1:gpt-x",
              "25",
              "80.0K",
              "40.6250",
              "0.1300",
              "2",
              "93.3%",
              "5.0s"
            ],
            [
              "anthropic-messages:prov2:claude-y",
              "4",
              "8.0K",
              "-",
              "-",
              "0",
              "100.0%",
              "-"
            ]
          ],
          "Notes": [
            "> 失败耗时⭐ = 该端点**失败尝试**累计墙钟时间：请求最终由别处完成，这段时间是纯粹的延迟损耗。\n> **只记时间、不折算成钱**：失败尝试拿不到 usage（vmr 只从客户端真正收到的那份响应里提取），厂商通常也不对失败请求计费——\n> 给它标一个金额会是编造。这里的口径是「它让你多等了多久」，不是「它花了你多少钱」。\n> 成本/1M out 用于横向比价（同样产出 100 万 token 谁更便宜）；成本/成功请求受各端点承接的请求形态影响，跨端点比较前先看 §5 的负载画像。\n\n"
          ]
        }
      ]
    },
    {
      "ID": "compactions",
      "Title": "§6.7 Compaction 还原 ⭐",
      "Blocks": [
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "时间",
            "压缩会话",
            "续接会话",
            "tokens_in → tokens_out",
            "保留比",
            "吞掉的实体（样例）"
          ],
          "Rows": [
            [
              "2026-07-23 02:00:00",
              "l-abc12345",
              "l-def67890",
              "120.0K → 9.0K",
              "8.0%",
              "e1, e2, e3 (+1 more)"
            ]
          ],
          "Notes": [
            "> 保留比 = tokens_out / tokens_in；保留比 ≥ 100% 表示输出未缩小（可能为规则识别假阳性或结构化扩展）。\n\n"
          ]
        }
      ]
    },
    {
      "ID": "efficiency",
      "Title": "§7 效率与浪费 ⭐",
      "Blocks": [
        {
          "Text": "> **累计发出** 48.0 KB · **其中死重** 24.0 KB (50%) · **≈ 浪费 token** 6.0K · **工具集形态** 1\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "发现",
            "指标",
            "值",
            "涉及",
            "建议"
          ],
          "Rows": [
            [
              "缓存未命中输入",
              "cache_miss_tokens",
              "150.0K (30.0%)",
              "全局，coding 占 150.0K",
              "检查 prompt 前缀稳定性 / 开启 provider 缓存"
            ],
            [
              "定时任务冗余",
              "fresh + cache_eff",
              "80.0K fresh, 缓存效率 11.1%",
              "heartbeat",
              "拉长间隔 / 换便宜模型 / 缓存前缀"
            ],
            [
              "输出截断",
              "truncated",
              "1/50",
              "stream 中断",
              "排查上游超时 / 提高 stream_idle"
            ],
            [
              "慢请求",
              "slow_request_share",
              "~5% > 30s",
              "见 §4 stream_ms 归因",
              "见 §4"
            ],
            [
              "额度即将耗尽",
              "provider_quota_used_pct",
              "138.9%（tokens · ）",
              "prov1",
              "检查该账户或模型的路由权重或额度配置"
            ]
          ],
          "Notes": null
        },
        {
          "Text": "**工具形态浪费 Top-5**（按浪费字节降序；完整明细见 vmr-report.json -> tools[]）\n\n"
        },
        {
          "Title": "",
          "Fold": "",
          "Headers": [
            "形态",
            "请求",
            "声明",
            "已用",
            "利用率",
            "浪费字节"
          ],
          "Rows": [
            [
              "tools:4",
              "6",
              "4",
              "2",
              "50.0%",
              "24.0 KB"
            ]
          ],
          "Notes": [
            "<details><summary>tools:4 · 6 请求 · 声明 4 个 · 实际调用 2 个</summary>\n\n**调用过的工具（2 个，按调用次数降序）：**\n\n1. read (5 次)\n2. exec (2 次)\n\n**声明但从未调用（2 个，按字母序）：**\n\n1. search\n2. write\n\n</details>\n\n",
            "> 统计窗口 = 本报告的输入日志范围；低频工具（如 cron 触发类）可能不在窗口内，裁剪决策建议基于 ≥1 周日志。\n\n"
          ]
        }
      ]
    },
    {
      "ID": "request-index",
      "Title": "§8 请求详单",
      "Blocks": [
        {
          "Text": "每条记录（Chat User -> Session -> Task -> Turn）见 [vmr-requests.md](./vmr-requests.md)。\n\n"
        },
        {
          "Text": "本次运行未生成 ` + "`" + `details/*.md` + "`" + `（默认按需生成）。用坐标（` + "`" + `vmr-requests.md` + "`" + ` 的『文件』列，未生成详单时显示为该坐标，形如 ` + "`" + `basename:line` + "`" + `）随时取出单条记录：` + "`" + `vmr replay -print -req <坐标>` + "`" + `，例如 ` + "`" + `vmr replay -print -req a.jsonl:3` + "`" + `；或加 ` + "`" + `-details` + "`" + ` 全量生成。\n\n"
        }
      ]
    },
    {
      "ID": "appendix",
      "Title": "附录 数据源与方法论",
      "Blocks": null
    }
  ],
  "Disclaimers": [
    "- 输入: a.jsonl, b.jsonl · format 11 · 7 记录 / 1 坏行\n",
    "- 时段: 2026-07-22 18:39:00 – 2026-07-24 02:00:00 (本地时区)\n",
    "- 百分位: nearest-rank\n",
    "- n 基准: 每个百分位标注 n（= ttft_known / requests_with_dur / stream_known）；n<20 标 ⚠️low-n。\n",
    "- 比值低置信度: cache_efficiency 等比值指标的分母 / 总请求数 < 90% 时标注脚注 ¹。\n",
    "- ⭐ 标记: 该列为衍生/预估指标（非上游直接返回值），解读时请结合样本量与口径说明。\n",
    "- 计费口径: fresh + cache_write(溢价) + out；缓存命中按各厂免费/极低价。\n",
    "- 慢请求阈值: 30s\n"
  ],
  "Footnotes": [
    {
      "ID": "self-traffic",
      "Text": "- 自指流量: 排除已启用，本次从全部统计中排除 2 条 ` + "`" + `vmr story -llm-addr` + "`" + ` 自身产生的分析请求（` + "`" + `-include-self-traffic` + "`" + ` 可关闭）。\n"
    },
    {
      "ID": "client-reconciliation",
      "Text": "- 客户端对账: 成本/负载表中存在但未独立生成 sibling 文件的客户端（仅含单发定时任务，已并入 cron 汇总）：claw-a。\n"
    }
  ]
}
`
)
