@echo off
cd /d "%~dp0.."
powershell -NoProfile -ExecutionPolicy Bypass -File "scripts\start_release.ps1" start
if errorlevel 1 (
    echo MedTrust could not start. Check whether ports 7001-7003 and 8001-8003 are available.
    pause
    exit /b 1
)
start "" "http://127.0.0.1:8001/"
