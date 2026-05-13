FROM golang:1.26.2-bookworm AS base

ENV \
    CGO_ENABLED=0 \
    GOOS=linux \
    LC_ALL=C.UTF-8

WORKDIR /app

RUN --mount=type=ssh apt-get update && apt-get install -y \
      git \
      openssh-client \
    && apt-get purge -y --auto-remove -o APT::AutoRemove::RecommendsImportant=false \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p -m 0700 ~/.ssh && ssh-keyscan github.com | sort > ~/.ssh/known_hosts

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .


# dev-shell ------------------------------

FROM base AS devel_shell

WORKDIR /app

COPY . /app

RUN --mount=type=ssh apt-get update && apt-get install -y \
      zsh \
      vim nano \
      python3 python3-pip python3-yaml \
    && sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" \
    && echo "source /root/.profile" >>/root/.zshrc \
    && pip3 install --user --break-system-packages pre-commit git-up \
    # Map the special names to docker host internal ip because 127.0.0.1 is *container* localhost on login
    && echo "sed 's/.*localmaeher.*//g' /etc/hosts >/etc/hosts.new && cat /etc/hosts.new >/etc/hosts" >>/root/.profile \
    && echo "echo \"\$(getent hosts host.docker.internal | awk '{ print $1 }') localmaeher.pvarki.fi mtls.localmaeher.pvarki.fi\" >>/etc/hosts" >>/root/.profile \
    && true

ENTRYPOINT ["/bin/zsh", "-l"]


# builder ------------------------------

FROM base AS builder

WORKDIR /app

# Build the kraftwerk
RUN go build -o cmd/kc_client_init kc_client_init.go

# Move to /dist directory AS the place for resulting binary folder
WORKDIR /dist

# Copy binary from build to main folder
RUN cp /app/cmd/kc_client_init .


# production ------------------------------

FROM scratch AS production

COPY --from=builder /dist/kc_client_init /

# Command to run the executable
ENTRYPOINT ["/kc_client_init"]
