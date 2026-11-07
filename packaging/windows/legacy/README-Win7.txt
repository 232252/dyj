dyj legacy 版 (Windows 7 SP1 / 8 / 8.1)
========================================

安装
----
1. 解压本目录到固定位置 (建议 C:\dyj, 不要放在需要清理的临时目录);
2. 右键 install-legacy.bat → 以管理员身份运行 (脚本会自动请求提权);
3. 完成后 dyj 以系统服务常驻, dyj-tray.exe 登录自启。

卸载: 右键 uninstall-legacy.bat → 以管理员身份运行。
配置与打印历史在 %ProgramData%\dyj, 卸载不删除, 可手动清理。

系统要求
--------
- 最低 Windows 7 SP1; Windows 8 / 8.1 / Server 2008 R2+ 同样适用。
- XP / Vista 不支持。
- 32 位 (x86) 与 64 位 (x64) 均有对应 zip, 请按系统位数下载。

角色说明 (重要)
--------
legacy 版仅支持「打印服务器」角色: 这台老电脑接着打印机,
手机 / 新电脑无线打印到它。

在老电脑上"添加 dyj 网络打印机"(客户端/driverless 功能)需要
Windows 8+ 的 PowerShell 打印 cmdlet 与 Windows 10 21H2+ 的
Microsoft IPP Class Driver, Win7 上不存在, 因此不可用。

管理页面
--------
- 从其他设备 (手机 / 新电脑) 的浏览器访问 http://这台电脑IP:6631/admin
- Win7 本机的 IE11 不受支持 (页面空白是正常现象);
  如确需本机操作, 可安装 Chrome 109 或更早版本 (Win7 上限)。

常见问题
--------
- 打印机没被其他设备发现: 检查 install-legacy.bat 是否以管理员运行
  (UDP 5353 防火墙规则), 以及路由器是否开启了 AP 隔离。
- 服务未启动: 手动运行 dyj.exe service start 查看报错;
  日志在 %ProgramData%\dyj\。
- 远程管理要求 token: token 在 %ProgramData%\dyj\admin-token,
  首次启动时也会打印在服务日志里。
