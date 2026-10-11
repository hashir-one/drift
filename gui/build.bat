@echo off
setlocal
cd /d "%~dp0"
where gcc >nul 2>nul
if errorlevel 1 (
    if exist "C:\gcc\mingw64\bin\gcc.exe" set "PATH=C:\gcc\mingw64\bin;%PATH%"
)
where gcc >nul 2>nul
if errorlevel 1 (
    echo GCC was not found. Install a MinGW-w64 compiler and add its bin folder to PATH.
    exit /b 1
)
set "CGO_ENABLED=1"
echo Building Drift...
go run ./tools/brand
if errorlevel 1 exit /b 1
windres -i assets/drift.rc -O coff -o resource_windows_amd64.syso
if errorlevel 1 exit /b 1
go build -trimpath -ldflags="-H windowsgui -s -w" -o "drift.exe" .
if errorlevel 1 exit /b 1
echo Ready: %~dp0drift.exe
exit /b 0
