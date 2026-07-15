set PATH=D:\mingw64\bin;%PATH%
set CGO_ENABLED=1
cd /d e:\work\desktop_go
%GOPATH%\bin\rsrc -manifest desktop_go.exe.manifest -ico internal/resources/app.ico -o rsrc.syso
go build -tags walk_use_cgo -ldflags="-H windowsgui" -v -o desktop_go.exe ./main.go
:: desktop_go.exe -d