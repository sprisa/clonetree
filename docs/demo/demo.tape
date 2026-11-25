# README recording and UI check. Run docs/demo/record.sh rather than vhs directly.
#
# VHS writes one text frame per command, so live views are sampled with short
# Sleep commands; record.sh checks those frames for layout jumps.
Output docs/assets/demo.gif
Output docs/demo/.frames/demo.txt

Set Width 1200
Set Height 840
Set FontSize 18

Source docs/demo/theme.tape

Sleep 600ms
Type "clonetree"
Sleep 300ms
Enter
Sleep 4s
Type "clear" Enter
Sleep 300ms
Type "clonetree new my-feature"
Sleep 400ms
Enter
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Wait+Screen /cd clonetree/
Sleep 2.2s

Type "clonetree ls"
Sleep 300ms
Enter
Sleep 2.4s

Type "clear" Enter
Type "clonetree info"
Sleep 300ms
Enter
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Sleep 100ms
Wait+Screen /measured in/
Sleep 4s
