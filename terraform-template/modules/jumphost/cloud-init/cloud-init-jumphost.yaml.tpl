#cloud-config

hostname: jumphost
fqdn: ${fqdn}

users:
  - name: ubuntu
    groups:
      - sudo
      - docker
    shell: /bin/bash
    #ssh_authorized_keys:
    #  - dollar{ssh_public_key}
    # review the following line from a security perspective.
    sudo: ALL=(ALL) NOPASSWD:ALL

package_update: true
packages:
  - net-tools
  - curl
  - htop
  - openssh-server
  - docker.io
  - gnupg
  - software-properties-common
  - golang
  - npm
  - nodejs
  - unzip
  - kubectx
  - emacs
  - vim
  - tmate
  - fzf
  - dc
  - mosh
  - wireguard
  - caddy
 
# This ubuntu image by default comes up with dysfunctional DNS
# lookups. This bootcmd sequence is a workaround.
  
bootcmd:
  - systemctl stop systemd-resolved
  - systemctl disable systemd-resolved
  - systemctl mask systemd-resolved
  - rm /etc/resolv.conf
  - echo nameserver 8.8.8.8 > /etc/resolv.conf
  - echo 127.0.0.1 jumphost.localdomain jumphost >> /etc/hosts
  - echo "${fqdn}" > /etc/fqdn

# Deferred: written after package installation, so the caddy package
# does not overwrite our Caddyfile.
write_files:
  - path: /etc/caddy/Caddyfile
    defer: true
    content: |
      ${servername} {
        reverse_proxy localhost:8000
      }
  - path: /etc/systemd/system/gotty.service
    defer: true
    permissions: '0600'
    content: |
      [Unit]
      Description=GoTTY web terminal (ssh to localhost)
      After=network-online.target ssh.service
      Wants=network-online.target

      [Service]
      User=ubuntu
      WorkingDirectory=/home/ubuntu
      ExecStart=/usr/local/bin/gotty -a 127.0.0.1 -p 8000 -c shell:${default_jumphost_password} -w ssh -o StrictHostKeyChecking=accept-new localhost
      Restart=always
      RestartSec=3

      [Install]
      WantedBy=multi-user.target

runcmd:
  - systemctl enable --now ssh || systemctl enable --now sshd
  # ubuntu needs an ssh key which is also authorized for itself (used by gotty's "ssh localhost")
  - sudo -u ubuntu bash -c 'mkdir -p -m 700 ~/.ssh; test -f ~/.ssh/id_ed25519 || ssh-keygen -q -t ed25519 -N "" -f ~/.ssh/id_ed25519; grep -qxFf ~/.ssh/id_ed25519.pub ~/.ssh/authorized_keys 2>/dev/null || cat ~/.ssh/id_ed25519.pub >> ~/.ssh/authorized_keys; chmod 600 ~/.ssh/authorized_keys'
  - curl -L https://github.com/yudai/gotty/releases/download/v1.0.1/gotty_linux_amd64.tar.gz | tar xz -C /usr/local/bin ./gotty
  - systemctl daemon-reload
  - systemctl enable --now gotty
  - systemctl restart caddy
  - echo "done" > /tmp/cloud-config.done

timezone: Europe/Berlin
keyboard:
  layout: de
  variant: ""

# The following allows password-only access in case ssh keys
# are not working for you. We use a strong password generated
# by terraform. It is recommended to disallow all
# password authentification for hardening.

ssh_pwauth: True
chpasswd:
  expire: false
  users:
  - name: ubuntu
    password: ${default_jumphost_password}
    type: text
  
