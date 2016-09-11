FROM debian:jessie

RUN apt-get update && \
  apt-get upgrade -y && \
  apt-get install -y ca-certificates && \
  apt-get clean -y && \
  apt-get autoclean -y && \
  apt-get autoremove -y && \
  rm -rf /usr/share/locale/* && \
  rm -rf /var/cache/debconf/*-old && \
  rm -rf /var/lib/apt/lists/* && \
  rm -rf /usr/share/doc/*
  
ADD caddy /usr/bin/caddy

CMD ["/usr/bin/caddy", "-conf=/etc/Caddyfile"]


