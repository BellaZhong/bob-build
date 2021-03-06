FOLD=""
STATUS_CODE=0

fold_start() {
    FOLD="$1"
    travis_fold start "$FOLD"
    travis_time_start
    echo -e "\e[33;1m$FOLD\e[0m"
}

fold_end() {
    travis_time_finish
    travis_fold end "$FOLD"
}

result_ok() {
    echo -e "\e[32;1mOK\e[0m"
}

result_skip() {
    local MSG=$1

    echo -n "$MSG "
    echo -e "\e[33;1mSKIP\e[0m"
}

result_fail() {
    echo -e "\e[31;1mFAIL\e[0m"
}

check_result() {
    local RESULT=$1
    local MSG=$2

    echo -n "$MSG "
    if [ $RESULT -eq 0 ]; then
        result_ok
    else
        STATUS_CODE=1
        result_fail
    fi
}
