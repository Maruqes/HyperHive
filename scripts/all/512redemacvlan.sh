LOWER=512rede
MACVLAN=host-macvlan
HOST_ALIAS_IP=192.168.76.2/24

sudo nmcli connection add type macvlan ifname $MACVLAN dev $LOWER mode bridge con-name $MACVLAN
sudo nmcli connection modify $MACVLAN ipv4.addresses "$HOST_ALIAS_IP" ipv4.method manual ipv6.method ignore
sudo nmcli connection modify $MACVLAN connection.autoconnect yes
sudo nmcli connection up $MACVLAN
