# Interest form draft: notify

> **DRAFT. Not submitted.** Working draft for issue [#64](https://github.com/romulospl/notify/issues/64). Nothing was sent to SCF and no external form was filled. The real form lives at <https://communityfund.stellar.org> and its exact fields were **not verified** (the drafter cannot open it). The fields below follow the form as described in the SCF Handbook "Build Award" page (Raven, 2026-11-15: the form asks for the track and whether you were referred) and the SCF interest form guidance. Check every field against the live form before copying.

## Open items for Romulo

1. **Referral code.** Not obtained. The RFP brief says builders unknown to SCF are not eligible (Raven `getRfps`, 2026-11-15), and the handbook says SCF relies heavily on referral signals, though a referral is not mandatory and a project without one still gets an initial review (SCF Handbook "Build Award", 2026-11-15). Decide: look for a referrer, or record in #64 the decision to send without one. The SCF interest form guidance (a skill reference, not the handbook) says the interest form must be submitted **before** a referrer can fill in the referral form, because the referrer picks the project by name from submitted interest forms. This ordering rule is **UNVERIFIED**.
2. **Team** (names, roles, links). Not in the repository.
3. **Budget range.** Proposal only; see [`proposal.md`](proposal.md) section 5. Choose the range after Romulo validates the budget.
4. **Jurisdiction and legal entity.** Handbook: jurisdiction-specific restrictions may apply, and SCF reaches out after reading the form.
5. **Timing.** SCF #46 is in Initial Review with its deadline passed (2026-11-09); the next round window is not stated (Raven `getRfps`, 2026-11-15). The handbook says forms are reviewed on a rolling basis and to submit early to be considered for a given round. Record the deadline of the round you are invited to, as #64 asks.
6. **Language.** Write the final form in English. The repository documents linked below are in Portuguese; see item 5 of [`proposal.md`](proposal.md) section 0.

## Draft fields

**Track:** RFP Track

**RFP:** Web and API Analytics for Stellar Ecosystem Projects (Q4 2026)

**Referral code:** `[PENDING: referral code, or "none"]`

**Project name:** notify

**One-line description:**
An open source, privacy-first web and API analytics service that SCF runs for Stellar ecosystem projects, giving each project private usage data and SCF a trusted global view, built on Umami and PostgreSQL.

**Project description:**

SCF has no trusted, unified source of end-user usage data for the projects it funds, so reviewers weigh follow-on programs on self-reported metrics. The RFP asks for a multi-tenant service, free for Stellar projects, that measures real usage on the web and on APIs without cookies or personal tracking.

notify is that service. Web usage comes from a version-pinned tracker script (with Subresource Integrity), a Go gateway that checks a JavaScript proof of work, limits fingerprints per IP and truncates the IP before it reaches Umami. API usage comes from OpenTelemetry dual-export to a custom collector that keeps only standardized metrics (service, normalized route, status code and latency) in PostgreSQL and discards traces, spans and logs. Each project sees only its own data; SCF operators see all projects and can compare them.

The repository is public on MIT license and already contains the portal, tracker, gateway, collector, rollup job, dashboards, infrastructure code and an isolation test suite. What is not done yet: the production environment has not been applied, the API collector is not in the production stack, there are no pilots, and load, availability and handover work are ahead. The proposal funds that remaining work and 12 months of hosting and support.

**Why Umami (not GoatCounter):** MIT license approved by OSI, PostgreSQL only (which the API metrics need), a native tenant and site model and an active maintainer team. Decision record: [`docs/adr/0001-ferramenta-base.md`](../adr/0001-ferramenta-base.md).

**Stellar integration:**

This is ecosystem infrastructure for Stellar projects, required by an SCF RFP, not an on-chain application. It uses no Soroban contract. State that plainly. The Stellar tie is the audience and the purpose: instrumenting SCF-funded projects, giving SCF the usage data that its follow-on programs (Growth Hack, Public Goods Award, Integration Track traction) need, per the RFP brief (Raven `getRfps`, 2026-11-15). `[DRAFT NOTE: do not claim Soroban or SEP use.]`

**Team:**

- `[Name], [role], [one sentence of relevant experience], [GitHub or LinkedIn link]`
- `[Name], [role], [one sentence of relevant experience], [GitHub or LinkedIn link]`

`[DRAFT NOTE: lead with any prior Stellar, SCF or developer tooling work. Mention a delivered SCF award if there is one.]`

**Current stage:** Prototype running locally and in CI. Not in production. No users yet.

Evidence: public repository with ADRs, specs and tests; architecture and plain-language explanation in [`docs/arquitetura.md`](../arquitetura.md); current status in [`docs/andamento.md`](../andamento.md). `[DRAFT NOTE: replace the relative links with the public repository URL on submission.]`

**Pilots and demand:** No pilot or letter of interest yet (issue [#44](https://github.com/romulospl/notify/issues/44)). The RFP requires at least three SCF-awarded projects on web and one on API. Pilot recruiting is planned; names will be added only with a letter.

**Requested budget range:** `[PENDING Romulo]`. The proposal draft ([`proposal.md`](proposal.md)) uses USD 150,000 as an illustrative ceiling (handbook: Build Award up to USD 150,000, Raven 2026-11-15), split 10/20/30/40 across tranches. The total is a proposal to validate, not a quote.

## Review against the interest form guidance

| Check | Result |
|---|---|
| One-line description answers what, for whom and why Stellar | The Stellar answer is the audience (SCF-funded projects) |
| Stellar's role is specific | Specific and honest: no on-chain component |
| Problem is concrete | Yes, from the RFP brief |
| Team named with experience | **No, placeholder** |
| Stage is honest | Yes |
| Budget range matches scope | To validate |
| No buzzwords | Checked |
| Every claim accurate | Checked against `docs/andamento.md` on 2026-11-15 |
| Links tested | Relative links validated in the repository; replace with public URLs on submission |

Interest form guidance also says an idea-stage team is weakest and a team without a named group rarely advances. This draft is stronger on stage (working code) and weaker on team, referral and pilots.
