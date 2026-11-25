# Static GitHub social preview (1280 × 640). Run docs/demo/record.sh.
Output docs/demo/.social.gif

Set Width 1280
Set Height 640
Set FontSize 20

Source docs/demo/theme.tape

Hide
Type "clear; banner"
Enter
Sleep 300ms
Show
Type "clonetree info" Enter
Wait+Screen /measured in/
Sleep 500ms
Screenshot docs/assets/social-terminal.png
# VHS saves a screenshot with the next frame.
Sleep 300ms
