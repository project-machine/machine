# machined


machined install
- if systemd system:
    - write machined.socket/machined.service systemd units to user path
    $XDG_CONFIG_HOME/systemd/user/
    - calls systemctl --user daemon-reload
    - calls systemctl --user enable machined.socket
    - calls systemctl --user enable machined.server

# install to user systemd config path
machined install

# overwite systemd units
machined install --force

# installs to host systemd path
machined install --host

# remove installed units
machined remove

# remove installed units from host systemd path
sudo machined remove --host
