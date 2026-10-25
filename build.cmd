@echo off
rem 重新编译 bili-sub.exe（需要 Go 1.22+，并且 go 已经在 PATH 里）
cd /d "%~dp0"
go build -trimpath -ldflags "-s -w" -o bili-sub.exe .
if errorlevel 1 exit /b %errorlevel%
echo built: %~dp0bili-sub.exe
