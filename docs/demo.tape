Output assets/demo.gif
Set Shell "bash"
Set FontFamily "Menlo"
Set FontSize 18
Set Width 1280
Set Height 760
Set Padding 28
Set Framerate 20
Set CursorBlink false
Set Theme {"name":"Launchproof","black":"#101611","red":"#E88C83","green":"#C5EF91","yellow":"#E5D59B","blue":"#A6BEC9","magenta":"#B8ABC5","cyan":"#99C7B5","white":"#E8EBE3","brightBlack":"#737E77","brightRed":"#F0B0A8","brightGreen":"#D4F7AE","brightYellow":"#EEE2BA","brightBlue":"#C4D9E1","brightMagenta":"#D1C5DB","brightCyan":"#C2E2D4","brightWhite":"#F4F6ED","background":"#101611","foreground":"#E8EBE3","selection":"#35433B","cursor":"#C5EF91"}

Env TERM "xterm-256color"
Env CI ""
Env NO_COLOR ""
Env ACCESSIBLE ""

Hide
Type "./bin/launchproof"
Enter
Sleep 500ms
Show
Sleep 4s
Screenshot assets/welcome.png
Down
Sleep 500ms
Enter
Sleep 2s
Escape
Sleep 500ms
Type "i"
Sleep 2s
Screenshot assets/inspect.png
Sleep 1s
Type "q"
