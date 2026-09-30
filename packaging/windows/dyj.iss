; dyj Windows installer (Inno Setup 6.4+).
; Build: iscc packaging\windows\dyj.iss  (GitHub windows runners ship ISCC)
; Policy (docs/research/10 §3):
;   - per-machine install (service + machine firewall rules require it);
;   - firewall rules are pre-created for ALL THREE profiles — the
;     first-listen dialog defaults to private only, and small-office
;     networks are frequently misclassified as public;
;   - uninstall stops the tray + service, removes rules and the Run
;     key; %ProgramData%\dyj is kept unless the user opts in.

#define AppName "dyj"
#define AppVersion GetEnv("DYJ_VERSION")
#if AppVersion == ""
  #define AppVersion "0.2.0"
#endif
#define DistRoot "..\..\dist"

[Setup]
AppId={{8A6E1F3B-2C44-4B7D-9A51-DYJ00000000}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher=dyj maintainers
DefaultDirName={autopf}\dyj
DefaultGroupName=dyj
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64compatible arm64
OutputDir={#DistRoot}
OutputBaseFilename=dyj-setup-x64
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
UninstallDisplayIcon={app}\dyj-tray.exe
LicenseFile=..\..\LICENSE

[Files]
Source: "{#DistRoot}\dyj-windows-amd64.exe"; DestDir: "{app}"; DestName: "dyj.exe"; Flags: ignoreversion; Check: NotArm64
Source: "{#DistRoot}\dyj-tray-windows-amd64.exe"; DestDir: "{app}"; DestName: "dyj-tray.exe"; Flags: ignoreversion; Check: NotArm64
Source: "{#DistRoot}\dyj-windows-arm64.exe"; DestDir: "{app}"; DestName: "dyj.exe"; Flags: ignoreversion; Check: IsArm64
Source: "{#DistRoot}\dyj-tray-windows-arm64.exe"; DestDir: "{app}"; DestName: "dyj-tray.exe"; Flags: ignoreversion; Check: IsArm64

[Dirs]
Name: "{commonappdata}\dyj"; Permissions: users-modify

[Icons]
Name: "{group}\dyj 打印机共享"; Filename: "{app}\dyj-tray.exe"; Comment: "打开 dyj 托盘与管理页"
Name: "{group}\dyj 设置"; Filename: "{app}\dyj.exe"; Parameters: "open"; Comment: "向导 / 管理页（首次运行选角色）"
Name: "{commondesktop}\dyj"; Filename: "{app}\dyj.exe"; Parameters: "open"; Comment: "dyj 设置与管理页"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式（dyj 设置）"; GroupDescription: "附加任务："

[Run]
; --- firewall: program-scoped rules on every profile, before anything listens
Filename: "netsh"; Parameters: "advfirewall firewall add rule name=""dyj IPP"" dir=in action=allow program=""{app}\dyj.exe"" protocol=TCP localport=6631 profile=domain,private,public"; Flags: runhidden
Filename: "netsh"; Parameters: "advfirewall firewall add rule name=""dyj mDNS"" dir=in action=allow program=""{app}\dyj.exe"" protocol=UDP localport=5353 profile=domain,private,public"; Flags: runhidden
; --- tray autostart for every user (HKLM Run) — opt-out checkbox below
Filename: "reg"; Parameters: "add HKLM\Software\Microsoft\Windows\CurrentVersion\Run /v dyj-tray /t REG_SZ /d ""\""{app}\dyj-tray.exe\"""" /f"; Flags: runhidden; Check: WantAutostart
; --- register + start the service with current settings
Filename: "{app}\dyj.exe"; Parameters: "service install"; Flags: runhidden; StatusMsg: "注册 dyj 服务…"
Filename: "{app}\dyj.exe"; Parameters: "service start"; Flags: runhidden; StatusMsg: "启动 dyj 服务…"
Filename: "{app}\dyj-tray.exe"; Description: "启动 dyj（托盘 + 管理页向导）"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "taskkill"; Parameters: "/IM dyj-tray.exe /F"; Flags: runhidden; RunOnceId: "KillTray"
Filename: "{app}\dyj.exe"; Parameters: "service stop"; Flags: runhidden; RunOnceId: "StopSvc"
Filename: "{app}\dyj.exe"; Parameters: "service uninstall"; Flags: runhidden; RunOnceId: "DelSvc"
Filename: "netsh"; Parameters: "advfirewall firewall delete rule name=""dyj IPP"""; Flags: runhidden; RunOnceId: "DelRuleIPP"
Filename: "netsh"; Parameters: "advfirewall firewall delete rule name=""dyj mDNS"""; Flags: runhidden; RunOnceId: "DelRuleMDNS"
Filename: "reg"; Parameters: "delete HKLM\Software\Microsoft\Windows\CurrentVersion\Run /v dyj-tray /f"; Flags: runhidden; RunOnceId: "DelRun"

[UninstallDelete]
; spool/UUID/token stay in {commonappdata}\dyj unless the user opts in
; via the checkbox in [Code].

[Code]
var
  DeleteDataCheckbox: TNewCheckBox;

function IsArm64(): Boolean;
begin
  Result := ProcessorArchitecture = paARM64;
end;

function NotArm64(): Boolean;
begin
  Result := not IsArm64;
end;

function WantAutostart(): Boolean;
begin
  Result := not IsUninstaller();
end;

procedure CurUninstallPrepare();
begin
  // Nothing interactive here: the checkbox lives on the finished page.
end;

procedure CurUninstallStep(Step: TUninstallStep);
begin
  if Step = usPostUninstall then
  begin
    if MsgBox('是否同时删除配置与打印历史？(%ProgramData%\dyj)', mbConfirmation, MB_YESNO) = IDYES then
      DelTree(ExpandConstant('{commonappdata}\dyj'), True, True, True);
  end;
end;
