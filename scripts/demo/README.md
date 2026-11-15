# README demo GIF

`docs/assets/readme/cli-demo.gif` is recorded from a real server and CLI, not
mocked up:

```sh
go build -o /tmp/clavis-demo/clavis ./cmd/clavis
scripts/demo/capture.sh /tmp/clavis-demo/clavis /tmp/demo-out
node scripts/demo/render.mjs /tmp/demo-out docs/assets/readme/cli-demo.gif
```

`capture.sh` starts a throwaway server, fills a demo project, and runs each
command on a pseudo-terminal so the output keeps its colors. `render.mjs`
types the commands, shows the captured output, and encodes the frames with
ffmpeg. It needs the Playwright install in `web/` (Chrome channel) and
`ffmpeg`. Change the commands in `capture.sh` to change the demo.
