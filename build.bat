@echo off
setlocal enabledelayedexpansion

:: Read version from VERSION file
set /p VERSION=<VERSION

:: Get current date using PowerShell
for /f "delims=" %%i in ('powershell -Command "Get-Date -Format 'yyyy-MM-ddTHH:mm:ssZ'"') do set BUILD_DATE=%%i

:: Create dist folder if it doesn't exist
if not exist dist mkdir dist

:: Build with version info
echo Building wusbkit v%VERSION%...
go build -ldflags "-X github.com/lazaroagomez/wusbkit/cmd.Version=%VERSION% -X github.com/lazaroagomez/wusbkit/cmd.BuildDate=%BUILD_DATE%" -o dist\wusbkit.exe .

if %ERRORLEVEL% EQU 0 (
    echo.
    echo Build successful: dist\wusbkit.exe
    echo Version: %VERSION%
) else (
    echo.
    echo Build failed!
    exit /b 1
)

endlocal
