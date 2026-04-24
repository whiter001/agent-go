# Autoskill optimization temporary design

## Status

This is a temporary implementation design for improving autoskill behavior in `agent-go`.
It documents the current gaps, the phased rollout plan, and the acceptance criteria for each phase.

Current progress in this workspace:

- Phase 1 is implemented
- Phase 2 core context extraction is implemented
- Phase 3 baseline draft generation is implemented
- Phase 4 baseline feedback ranking is implemented

## Current state

Today, autoskill in `agent-go` is primarily **retrieval-driven**:

- skill files are discovered from configured directories
- the loader scores skills against the current prompt
- the top matching skills are injected into turn context

What is **not** fully wired yet:

- promotion and cleanup workflows for generated draft skills
- richer usefulness inference beyond tool-overlap heuristics
- maintenance helpers for stale or overlapping autoskills

## Problems to solve

1. Skill ranking is too shallow
   - current matching is a substring count across `name + description + content`
   - all fields have nearly the same value
   - tools, tags, triggers, and section headings are not first-class ranking inputs

2. Mixed-language prompts are under-served
   - prompts like `执行autobrowser help` or `用pwsh跑check脚本` should split into meaningful tokens
   - current whitespace tokenization is weak for Chinese and mixed-language requests

3. Context injection is noisy
   - the loader injects a large truncated block of raw skill content
   - unrelated sections can crowd out the most relevant execution guidance

4. Auto-create needs smarter evolution
  - baseline draft generation is now available for successful traces
  - ranking feedback, promotion, and cleanup workflows are still missing

## Design goals

- improve top-k skill selection accuracy
- make mixed Chinese/English prompts more reliable
- reduce prompt noise by injecting only the most relevant sections
- prepare the data model for future auto-create and feedback loops
- keep the first rollout low-risk and fully testable

## Non-goals for the first rollout

- full semantic embedding search
- external vector databases
- automatic promotion of generated skills to permanent curated skills
- large-scale refactors across the whole agent runtime

## Phase plan

### Phase 1 — retrieval quality foundation

Scope:

- parse lightweight frontmatter from `SKILL.md`
- support structured fields such as:
  - `name`
  - `description`
  - `tags`
  - `tools`
  - `triggers`
  - `platform`
- improve tokenization for mixed Chinese/English prompts
- replace flat substring scoring with weighted scoring
- keep the public loader behavior compatible with the current runtime

Acceptance criteria:

- a skill can expose metadata without breaking old skill files
- prompts like `执行autobrowser help` can reliably favor skills mentioning `autobrowser`
- name/tool/tag matches outrank weak body-only matches

### Phase 2 — context precision

Scope:

- parse markdown sections from skill content
- rank sections separately from the whole document
- inject only the most relevant sections into turn context
- include compact metadata in the injected snippet when helpful

Acceptance criteria:

- turn context highlights the most relevant skill sections
- unrelated sections are less likely to be injected
- prompt token usage becomes more focused without losing key guidance

### Phase 3 — runtime feedback and draft generation

Implementation status: baseline draft generation is wired.

Scope:

- capture successful execution traces suitable for reuse
- generate draft autoskills into `auto_skill_dir`
- gate creation by minimum tool call count and basic deduplication
- keep generated skills in draft status first

Acceptance criteria:

- repeated successful workflows can generate draft skills
- noisy one-off workflows are filtered out
- generated drafts are readable and editable by humans

### Phase 4 — adaptive ranking and maintenance

Implementation status: selection feedback persistence and ranking bonuses are wired; cleanup and promotion flows remain pending.

Scope:

- log which skills were selected for each run
- track whether selected skills were actually useful
- support lightweight freshness and success metadata
- add cleanup and deduplication helpers for stale or overlapping autoskills

Acceptance criteria:

- frequently useful skills trend upward in ranking
- stale or ineffective skills can be identified and pruned

## Immediate implementation plan

The current coding pass implements **Phase 1**, the core of **Phase 2**, a baseline for **Phase 3**, and the first half of **Phase 4**:

1. add structured skill metadata parsing
2. add improved tokenization for mixed-language prompts
3. add weighted ranking over metadata and content
4. add section-aware context extraction
5. add draft autoskill generation from successful traces with duplicate protection
6. add selection feedback persistence and historical ranking bonuses
7. add tests covering frontmatter, ranking, mixed-language prompts, section selection, draft generation, and feedback persistence

## Risks and mitigations

- Risk: frontmatter parsing becomes too permissive
  - Mitigation: keep parsing intentionally lightweight and ignore unknown fields safely

- Risk: weighted scoring overfits to one language
  - Mitigation: preserve substring fallback while adding Chinese-friendly token expansion

- Risk: context extraction hides useful content
  - Mitigation: fall back to overview/first sections when no strong section match exists

## Follow-up after this pass

The next change set should focus on promotion/cleanup workflows, stronger usefulness inference, and maintenance flows for generated draft skills.
