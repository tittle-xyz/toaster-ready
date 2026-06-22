# Contributing

Thanks for your interest in `toaster-ready`.

## How it's developed (and the bar)

This project is built **AI-assisted and human-reviewed**, and is open about it — a tool
that measures whether a repo is ready for agents should be honest about being built with
them. What keeps the quality up, and what we ask of contributions:

- **Deterministic core.** The scoring binary has no LLM/agent in its path — it reads
  files, runs `git`, and (optionally) calls the GitHub API. Keep judgment, link
  resolution, and persistence out of the binary (those belong to the skill layer).
- **Decisions are recorded.** Non-trivial design choices go in an ADR (`docs/adr/`).
- **It scores itself.** `toaster-ready` must keep passing its own gate and scoring well on
  its own rubric — run `make gate`.

## Local workflow

```sh
make build        # -> ./bin/toaster
make check        # gofmt check + go vet + tests (run before committing)
make gate         # toaster-ready scores ITSELF and must pass its own floor
```

- Format with `gofmt` (`make fmt`); `make check` must be clean.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).
- Add tests for behavior changes; the scoring math and the no-data/not-applicable
  discipline are covered by tests — keep them green.

## License

By contributing, you agree that your contributions are licensed under the project's
[Apache-2.0](LICENSE) license (inbound = outbound).
