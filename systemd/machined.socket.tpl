[Unit]
Description=Machined Socket

[Socket]
ListenStream=%t/machined/machined.socket
SocketMode=0660

[Install]
WantedBy=sockets.target
