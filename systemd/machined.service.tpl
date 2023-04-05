[Unit]
Description=Machined Service
Requires=machined.socket
After=machined.socket
StartLimitIntervalSec=0

[Service]
Delegate=true
Type=exec
KillMode=process
ExecStart={{.MachinedBinaryPath}}

[Install]
WantedBy=default.target
