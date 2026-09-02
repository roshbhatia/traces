Require traces

Set Shell "zsh"
Set Theme "Catppuccin Macchiato"
Set FontSize 17
Set Width 1280
Set Height 620
Set Padding 22
Set Framerate 20
Set TypingSpeed 30ms

Sleep 1s

Type "traces -once -color always -provider , -file demo.json -session demo"
Enter
Wait+Screen /go test -race/
Sleep 4s
