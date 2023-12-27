# Go Telemetry

This repository holds the Go Telemetry server code and libraries.

## Linting & Formatting

This repository uses [eslint](https://eslint.org/) to format TS files,
[stylelint](https://stylelint.io/) to format CSS files, and
[prettier](https://prettier.io/) to format TS, CSS, Markdown, and YAML files.

See the style guides:

- [TypeScript](https://google.github.io/styleguide/tsguide.html)
- [CSS](https://go.dev/wiki/CSSStyleGuide)

It is encouraged that all TS and CSS code be run through formatters before
submitting a change. However, it is not a strict requirement enforced by CI.

### Installing npm Dependencies:

1. Install [docker](https://docs.docker.com/get-docker/)
2. Run `./npm install`

### Run ESLint, Stylelint, & Prettier

    ./npm run all

## Report Issues / Send Patches

This repository uses Gerrit for code changes. To learn how to submit changes to
this repository, see https://golang.org/doc/contribute.html.

The main issue tracker for the time repository is located at
https://github.com/golang/go/issues. Prefix your issue with "x/telemetry:" in
the subject line, so it is easy to find.
