# Copyright (c) 2026 Tom O'Connor <tom@twinhelix.org>
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

# bash completion for tb (Turnbacks CLI)
# Install:  tb completion bash > ~/.local/share/bash-completion/completions/tb
#     or:   echo 'eval "$(tb completion bash)"' >> ~/.bashrc   (works with macOS's bash 3.2)

# Shell-quote candidates (team and category names contain spaces, "&" and
# parentheses). A loop rather than "${COMPREPLY[@]//…}", which bash 3.2
# (macOS) joins into one word.
_tb_quote() {
    local i
    for i in "${!COMPREPLY[@]}"; do
        COMPREPLY[$i]=$(printf '%q' "${COMPREPLY[$i]}")
    done
}

_tb() {
    local cur prev cword
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    cword=$COMP_CWORD
    local IFS=$'\n'
    local commands="record
again
undo
problems
new
stats
teams
sync
config
completion
version
help"

    # Values for flags.
    case "$prev" in
        --team)
            COMPREPLY=( $(compgen -W "$(tb __complete teams 2>/dev/null)" -- "$cur") )
            _tb_quote
            return ;;
        --category)
            COMPREPLY=( $(compgen -W "$(tb __complete categories 2>/dev/null)" -- "$cur") )
            _tb_quote
            return ;;
        --period)
            COMPREPLY=( $(compgen -W $'7d\n30d\n90d\nyear' -- "$cur") )
            return ;;
        -n|--times|--note|--ago|--at|--alias)
            return ;;
    esac

    local aliases
    aliases="$(tb __complete problems 2>/dev/null | cut -f1)"

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "$commands"$'\n'"$aliases" -- "$cur") )
        return
    fi

    local cmd="${COMP_WORDS[1]}" pos=$cword record_flags=$'-n\n--times\n--note\n--ago\n--at\n--team\n--exact\n--yes'
    if [[ "$cmd" != record ]] && ! grep -qx -- "$cmd" <<<"$commands"; then
        # "tb <alias> <minutes> …" shorthand
        cmd=record
        pos=$((cword + 1))
    fi

    case "$cmd" in
        record)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "$record_flags" -- "$cur") )
            elif [[ $pos -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "$aliases" -- "$cur") )
            elif [[ $pos -eq 3 ]]; then
                COMPREPLY=( $(compgen -W $'1\n5\n10\n15\n30\n60' -- "$cur") )
            fi ;;
        again)      COMPREPLY=( $(compgen -W $'-n\n--yes' -- "$cur") ) ;;
        stats)      COMPREPLY=( $(compgen -W '--period' -- "$cur") ) ;;
        new)        COMPREPLY=( $(compgen -W $'--alias\n--category' -- "$cur") ) ;;
        completion) [[ $cword -eq 2 ]] && COMPREPLY=( $(compgen -W $'bash\nzsh' -- "$cur") ) ;;
        config)
            case $cword in
                2) COMPREPLY=( $(compgen -W $'show\nset\nunset' -- "$cur") ) ;;
                3) if [[ "${COMP_WORDS[2]}" == unset ]]; then COMPREPLY=( team ); else COMPREPLY=( $(compgen -W $'url\nteam' -- "$cur") ); fi ;;
                *) if [[ "${COMP_WORDS[3]}" == team ]]; then
                       COMPREPLY=( $(compgen -W "$(tb __complete teams 2>/dev/null)" -- "$cur") )
                       _tb_quote
                   fi ;;
            esac ;;
    esac
}
complete -F _tb tb
