set PATH=D:\mingw64\bin;%PATH%
set CGO_ENABLED=1
cd /d e:\work\desktop_go
D:\work\golang_git\gopath\bin\rsrc -manifest desktop_go.exe.manifest -ico internal/resources/app.ico -o rsrc.syso
go build -tags walk_use_cgo -gcflags="all=-N -l" -ldflags="-H windowsgui" -v -o desktop_go_debug.exe ./main.go
:: desktop_go_debug.exe -d
