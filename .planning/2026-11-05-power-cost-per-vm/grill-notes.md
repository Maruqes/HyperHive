# Power and cost per VM: Grill / Discovery Notes
Date: 2026-11-05 · Goal: settle milestone 1 of idea 3 (energy per VM, per slave and overall) so it can be planned.

## Summary / key decisions
- No dedicated Energy page (user, 2026-11-05). This overrides the "cluster energy dashboard" in Q18 of `notes/2026-11-03-hh-feature-ideas-grill.md`.
- Display: a small number on each VM card, an energy figure on each slave, and a general total on the dashboard page.
- The only user input is the electricity price, for example 0.21 per kWh. HH guesses power from the hardware (Q1). This replaces RAPL and fixed watts from Q13.
- Watt guess: formula of base board + CPU watts + RAM + disks, with CPU watts from a built-in CPU model to TDP table (Q2).
- Display value: this month's running total (kWh + EUR) plus live watts, on VM cards, slaves and the dashboard (Q3). Needs a new accumulated table.
- VM split: idle watts stay with the host; load watts split by each VM's CPU share. Stopped VMs show 0 W. VM totals will not sum to the slave total (Q4).
- M1 has no extras: no MCP tool, no power-schedule savings, no per-host watt override. User: "ml1 is suposde to be simple" (Q5).
- CPU table: `cpu-tdp-table.csv`, focused on Xeon and EPYC plus other Intel and AMD parts. Unknown CPUs fall back to core count and model family (Q6).
- GPUs are not counted in M1, so GPU hosts read low (Q7).

## Starting context (source-checked)
- No power reader exists in source (no RAPL, hwmon, IPMI or plug code).
- Master already stores per-host `cpu_snapshots` about every minute, kept 3 months (`master/db/info.go`, `master/services/info.go`).
- `virsh.proto` `Vm.currentCpuUsage` gives live VM CPU %; no per-VM history exists.
- `master/db/network_history.go` is a recent pattern for a bucketed history table.
- Earlier decisions Q13-Q18 (RAPL, p5 idle baseline, hourly Wh, 2-year retention) are from the 2026-11-03 session and may be replaced here.

## Q&A log

### Q1 — User input
- Asked: idle + max watts per slave, one fixed watts value, or RAPL + fixed fallback?
- Captured: none of these. User: "i want the system to be able to guess based on ram disks cpus etc etc etc... the user just says \"i pay 0.21 per kh\"". Only a price is entered; watts are estimated from detected hardware.
- Flags: none.

### Q2 — Watt guess method
- Asked: simple constants formula, CPU model TDP table, or formula plus RAPL?
- Captured: **CPU model TDP table.** Same formula, with CPU watts taken from a built-in table of models and TDPs. CPU model must be exposed from slave to master (read in `slave/info/cpu.go`, not in the proto today).
- Flags: fallback for unknown CPU models -> plan; table upkeep -> plan.

### Q3 — Number shown
- Asked: this month's kWh + EUR, live watts + projection, or both?
- Captured: **Both.** Monthly running total plus live watts. Master accumulates Wh per slave and per VM.
- Flags: compact layout on small VM cards -> plan.

### Q4 — VM split
- Asked: CPU share with idle to host, CPU + RAM share, or split everything?
- Captured: **CPU share, idle to host** (confirms 2026-11-03 Q14). The gap between VM totals and the slave total is host overhead.
- Flags: none.

### Q5 — M1 extras
- Asked: include a read-only MCP tool, power-schedule savings, or a per-host watt override?
- Captured: none. User: "none... ml1 is suposde to be simple".
- Flags: none.

### Q6 — CPU table scope
- Captured: user: "focus on xeons epycs some intels / amds (if it does not exist go by the core ammount and model)". User hardware: Xeon E5-2673 v4 and E5-2680 v4. Table built by an opencode DeepSeek agent; 17 well-known models spot-checked correct.
- Flags: Xeon E-2400 and D-1800/D-2800 missing, so they use the fallback.

### Q7 — GPUs
- Asked: keep GPUs out of the M1 estimate, or add GPU watts?
- Captured: **Keep GPUs out.**
- Flags: none.

## Open flags (pending input)
- Exact meaning of `currentCpuUsage` -> verify in slave source during Task 2.1.
- Formula constants -> check against a wall meter if one is available.
