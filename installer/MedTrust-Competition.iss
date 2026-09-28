#ifndef StageDir
  #error StageDir must be supplied to ISCC
#endif
#ifndef PackageDir
  #error PackageDir must be supplied to ISCC
#endif

[Setup]
AppId={{B1F8F76F-670D-4EB2-AE98-37D3A47B40E8}
AppName=MedTrust 比赛演示版
AppVersion=2026.09.28
AppPublisher=MedTrust 项目组
DefaultDirName={localappdata}\Programs\MedTrust
DefaultGroupName=MedTrust 比赛演示版
PrivilegesRequired=lowest
ArchitecturesAllowed=x64os
OutputDir={#PackageDir}
OutputBaseFilename=MedTrust-比赛演示版-Setup-20260928
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\medtrust-node.exe
InfoAfterFile={#StageDir}\README.md

[Tasks]
Name: "desktopicon"; Description: "在桌面创建启动图标"; GroupDescription: "其他选项："; Flags: unchecked

[Files]
Source: "{#StageDir}\medtrust-node.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\启动说明.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#StageDir}\configs\*"; DestDir: "{app}\configs"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#StageDir}\scripts\*"; DestDir: "{app}\scripts"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#StageDir}\web\dist\*"; DestDir: "{app}\web\dist"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#StageDir}\data\node-1\chain.db"; DestDir: "{app}\data\node-1"; Flags: onlyifdoesntexist uninsneveruninstall
Source: "{#StageDir}\data\node-1\attachments\*"; DestDir: "{app}\data\node-1\attachments"; Flags: onlyifdoesntexist uninsneveruninstall recursesubdirs createallsubdirs
Source: "{#StageDir}\data\node-2\chain.db"; DestDir: "{app}\data\node-2"; Flags: onlyifdoesntexist uninsneveruninstall
Source: "{#StageDir}\data\node-3\chain.db"; DestDir: "{app}\data\node-3"; Flags: onlyifdoesntexist uninsneveruninstall

[Icons]
Name: "{group}\启动并打开 MedTrust"; Filename: "{app}\scripts\启动并打开MedTrust.cmd"; WorkingDir: "{app}"
Name: "{group}\停止 MedTrust"; Filename: "{app}\scripts\停止MedTrust.cmd"; WorkingDir: "{app}"
Name: "{group}\使用说明"; Filename: "{app}\README.md"
Name: "{autodesktop}\MedTrust 比赛演示版"; Filename: "{app}\scripts\启动并打开MedTrust.cmd"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\scripts\启动并打开MedTrust.cmd"; Description: "现在启动并打开 MedTrust"; Flags: postinstall nowait skipifsilent unchecked

[UninstallRun]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\scripts\start_release.ps1"" stop"; Flags: runhidden; RunOnceId: "stop-cluster"
