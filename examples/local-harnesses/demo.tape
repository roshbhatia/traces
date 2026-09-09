Require traces
Set Shell "bash"
Set Theme "Catppuccin Macchiato"
Set FontSize 16
Set Width 1400
Set Height 660
Set Padding 20
Set Framerate 20
Set TypingSpeed 20ms
Hide
Type "export PS1='$ '; clear"
Enter
Show
Type "echo 'Offline transcript fixture; test output comes from a local run.'"
Enter
Sleep 1s
Type "traces --service codex --session token-review --once"
Enter
Wait+Screen /go test/
Sleep 3s
