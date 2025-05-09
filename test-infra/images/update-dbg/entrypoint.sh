#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Copyright (C) 2023 The KhulnaSoft Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#     http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

# Args from environment (with defaults)
GH_PROXY="${GH_PROXY:-"http://ghproxy"}"
GH_ORG="${GH_ORG:-"khulnasoft"}"
GH_REPO="${GH_REPO:-"test-infra"}"
BOT_NAME="${BOT_NAME:-"poiana"}"
BOT_MAIL="${BOT_MAIL:-"51138685+poiana@users.noreply.github.com"}"
BOT_GPG_KEY_PATH="${BOT_GPG_KEY_PATH:-"/root/gpg-signing-key/poiana.asc"}"
BOT_GPG_PUBLIC_KEY="${BOT_GPG_PUBLIC_KEY:-"EC9875C7B990D55F3B44D6E45F284448FF941C8F"}"

export GIT_COMMITTER_NAME=${BOT_NAME}
export GIT_COMMITTER_EMAIL=${BOT_MAIL}
export GIT_AUTHOR_NAME=${BOT_NAME}
export GIT_AUTHOR_EMAIL=${BOT_MAIL}

# Update the DBG from crawling results.
generate_driverkit_configs() {
    dbg-go configs cleanup -a amd64
    dbg-go configs generate -a amd64 --auto
    dbg-go configs cleanup -a arm64
    dbg-go configs generate -a arm64 --auto
}

# Sets git user configs, otherwise errors out.
# $1: git user name
# $2: git user email
ensure_git_config() {
    echo "> configuring git user (name=$1, email=$2)..." >&2
    git config --global user.name "$1"
    git config --global user.email "$2"

    git config user.name &>/dev/null && git config user.email &>/dev/null && return 0
    echo "ERROR: git config user.name, user.email unset. No defaults provided" >&2
    return 1
}

# Configures GPG key, otherwise errors out.
# $1: GPG key location
# $2: GPG ASCII armored public key
ensure_gpg_key() {
    echo "> configuring git with gpg key=$1..." >&2
    gpg --import "$1"
    git config --global commit.gpgsign true
    git config --global user.signingkey "$2"

    git config --global commit.gpgsign &>/dev/null && git config --global user.signingkey &>/dev/null && return 0
    echo "ERROR: git gpg key location, public key ID unset. No defaults provided" >&2
    return 1
}

# Creates a pull-request in case there are changes to commit and to push.
# $1: path of the file containing the token
create_pr() {
    nchanges=$(git status --porcelain=v1 2> /dev/null | wc -l)
    if [ "${nchanges}" -eq "0" ]; then
        echo "> moving on since there are no changes..." >&2
        return 0;
    fi

    echo "> creating commit..." >&2
    title="update(driverkit/config): add driverkit config for new kernels"
    git add .
    git commit -s -m "${title}"

    user=$(get_user_from_token "$1")
    branch="update/dbg"
    echo "> pushing commit as ${user} on branch ${branch}..." >&2
    git push -f \
        "https://${user}:$(cat "$1")@github.com/${GH_ORG}/${GH_REPO}" \
        "HEAD:${branch}"

    echo "> creating pull-request to merge ${user}:${branch} into master..." >&2
    body="This PR update the Driverkit Build Grid with the new released kernels. Do not edit this PR."

    pr-creator \
        --github-endpoint="${GH_PROXY}" \
        --github-token-path="$1" \
        --org="${GH_ORG}" --repo="${GH_REPO}" --branch=master \
        --title="${title}" --match-title="${title}" \
        --body="${body}" \
        --local --source="${branch}" \
        --allow-mods --confirm
}

# $1: path of the file containing the token
get_user_from_token() {
    curl --silent -H "Authorization: token $(cat "$1")" "https://api.github.com/user" | grep -Po '"login": "\K.*?(?=")'
}

# $1: the program to check
function check_program {
    if hash "$1" 2>/dev/null; then
        type -P "$1" >&/dev/null
    else
        echo "> aborting because $1 is required..." >&2
       return 1
    fi
}

# Meant to be run in the https://github.com/khulnasoft/fanal/test-infra repository.
# $1: path of the file containing the token
main() {
    # Checks
    check_program "gpg"
    check_program "git"
    check_program "curl"
    check_program "pr-creator"
    check_program "dbg-go"

    # Settings
    ensure_git_config "${BOT_NAME}" "${BOT_MAIL}"
    ensure_gpg_key "${BOT_GPG_KEY_PATH}" "${BOT_GPG_PUBLIC_KEY}"

    # Update the DBG from crawling results
    generate_driverkit_configs

    # Create PR (in case there are changes)
    create_pr "$1"
}

if [[ $# -lt 1 ]]; then
    echo "Usage: $(basename "$0") <path to github token>" >&2
    exit 1
fi

main "$@"
