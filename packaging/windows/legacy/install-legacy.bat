@echo off
REM dyj legacy (Win7 SP1/8/8.1) installer — portable-zip variant.
REM Modern Win10+ machines should use dyj-setup-x64.exe instead.
setlocal
set "DIR=%~dp0"
chcp 65001 >nul

REM --- self-elevate: service install + firewall rules need admin ---
net session >nul 2>&1
if %errorlevel% neq 0 (
    echo 需要管理员权限,正在请求提升...
    powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    exit /b
)

echo [1/4] 写入防火墙规则 (TCP 6631 / UDP 5353, 所有配置文件)...
netsh advfirewall firewall delete rule name="dyj IPP" >nul 2>&1
netsh advfirewall firewall add rule name="dyj IPP" dir=in action=allow program="%DIR%dyj.exe" protocol=TCP localport=6631 profile=domain,private,public >nul
netsh advfirewall firewall delete rule name="dyj mDNS" >nul 2>&1
netsh advfirewall firewall add rule name="dyj mDNS" dir=in action=allow program="%DIR%dyj.exe" protocol=UDP localport=5353 profile=domain,private,public >nul
if %errorlevel% neq 0 (
    echo 防火墙规则写入失败。
    pause
    exit /b 1
)

echo [2/4] 注册 dyj 服务...
"%DIR%dyj.exe" service install
if %errorlevel% neq 0 (
    echo 服务注册失败。
    pause
    exit /b 1
)

echo [3/4] 启动 dyj 服务...
"%DIR%dyj.exe" service start

echo [4/4] 托盘自启动 (当前用户)...
reg add HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v dyj-tray /t REG_SZ /d "\"%DIR%dyj-tray.exe\"" /f >nul

echo.
echo 安装完成。本机管理页面: http://127.0.0.1:6631/admin
echo 其他设备的浏览器请访问: http://本机IP:6631/admin
echo (Win7 自带的 IE11 不受支持, 请从手机或其他电脑打开管理页)
pause
