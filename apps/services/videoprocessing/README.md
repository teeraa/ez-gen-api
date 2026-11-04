# Local video assembly

`FFmpegProcessor.Merge` renders a real H.264/AAC MP4 with fast-start metadata.
It accepts 1–20 clips in the submitted order, validates trims against probed
media durations, and normalizes video to 30 FPS, square pixels and yuv420p.
Clips are fitted without cropping with letterboxing/pillarboxing. Resolution
selects the short edge (720 or 1080 pixels); supported aspect ratios are 16:9,
9:16, 1:1, 4:3, 3:4 and 21:9. Missing clip audio is replaced with stereo silence;
present audio is retained and normalized to 48 kHz. Background audio volume 0
means mute, not a default. Merge duration is capped at 20 minutes.

Runtime needs `ffmpeg` and `ffprobe` on PATH, an FFmpeg build with libx264 and
AAC encoding, and writable temporary disk. Merge has a 15-minute context limit;
caller cancellation terminates subprocesses and deferred cleanup removes local
temporary inputs created by the processor. Callers own their supplied inputs.
Output metadata and a full decode are checked before bytes are returned.
`ProbeVideo` performs metadata validation and full decode for provider outputs.

Text overlays additionally need FFmpeg's `drawtext` filter and a configured font
file (`NewFFmpegProcessor(fontFile)`). Empty font configuration defaults to
`/usr/share/fonts/dejavu/DejaVuSans.ttf`. `SupportsTextOverlays(ctx)` checks filter
and file presence, but cannot guarantee that a font contains every glyph.
Thai captions require a licensed Thai-capable font such as Prompt or Noto Sans
Thai; DejaVu alone is not a reliable Thai deployment configuration. Set the
existing renderer font configuration to that font's absolute file path.
Without these prerequisites, offer cuts/export without captions and explain
that captions are unavailable. Never replace missing glyphs with claimed success.
Text is written to a private file with FFmpeg expansion disabled rather than
interpolated into filter syntax. No shell is invoked.

Verification:

```sh
go test -race -v ./apps/services/videoprocessing
go vet ./apps/services/videoprocessing
go build ./apps/services/videoprocessing
```

Tests generate local colored/audio clips and run real FFmpeg/ffprobe. They make
no database, storage or AI-provider requests. Missing binaries explicitly skip
render tests; the optional overlay test explicitly skips missing drawtext/font.
On the current macOS FFmpeg 8.1.1 build, core render tests pass and drawtext is
unavailable, so overlay rendering is NOT_RUN. Docker font/filter capabilities
and Thai glyph rendering have not been runtime verified.
