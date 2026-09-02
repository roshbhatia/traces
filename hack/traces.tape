Require traces

Set Shell "zsh"
Set Theme "Catppuccin Macchiato"
Set FontSize 17
Set Width 1280
Set Height 720
Set Padding 22
Set Framerate 20
Set TypingSpeed 30ms

Sleep 1s

Type "cat demo.json | traces -color always -provider , -file - -session demo"
Enter
Wait+Screen /provider contract/
Sleep 2s
Type "k"
Sleep 1s
Enter
Sleep 2s
Type "l"
Sleep 1s
Type "j"
Sleep 1s
Type "q"
