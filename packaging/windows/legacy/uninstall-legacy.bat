@echo off
REM dyj legacy uninstaller — mirrors install-legacy.bat.
setlocal
set "DIR=%~dp0"
chcp 65001 >nul

net session >nul 2>&1
if %errorlevel% neq 0 (
    echo 需要管理员权限,正在请求提升...
    powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    exit /b
)

echo [1/3] 停止托盘与服务...
taskkill /IM dyj-tray.exe /F >nul 2>&1
"%DIR%dyj.exe" service stop >nul 2>&1
"%DIR%dyj.exe" service uninstall

echo [2/3] 移除自启动...
reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v dyj-tray /f >nul 2>&1

echo [3/3] 移除防火墙规则...
netsh advfirewall firewall delete rule name="dyj IPP" >nul 2>&1
netsh advfirewall firewall delete rule name="dyj mDNS" >nul 2>&1

echo.
echo 卸载完成。配置与打印历史 (%ProgramData%\dyj) 已保留, 可手动删除。
pause
