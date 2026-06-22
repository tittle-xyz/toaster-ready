# check fixtures

`badrepo/` is a deliberately terrible repository used to test that `toaster-ready` catches
a bad repo end-to-end: no README, no agent instructions, no CI, and a hardcoded
(fake) credential. It should land in the **needs-work** band and fail `toaster gate`.

These fixtures sit under `testdata/`, so `toaster-ready`'s own secret scanner skips them
when it scores this repo (any path containing "test" is ignored) — yet they score
normally when one of them is the repository being scored.
