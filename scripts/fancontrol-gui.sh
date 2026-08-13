#!/usr/bin/env bash
set -euo pipefail

CONF=/etc/fancontrol-gui.conf
INTERVAL=2
ENABLED=1
ACTIVE_MODE=1
FAN_SELECTION="1 1 1"
CURVE_FULL="20 100 35 100 50 100"
CURVE_COOL="20 20 40 40 60 60"
CURVE_QUIET="20 40 40 60 60 80"

source "${CONF}"

PWM_PATHS=()
TEMP_PATH=""

restore_pwm() {
  local i
  for i in "${!PWM_PATHS[@]}"; do
    [ -w "${PWM_PATHS[$i]}_enable" ] || continue
    # pwm_enable=2 is the it87 automatic mode. It is the safe hand-off
    # state after fancontrol-gui stops, including after a restart.
    printf '2\n' > "${PWM_PATHS[$i]}_enable" 2>/dev/null || true
  done
}
trap restore_pwm EXIT
trap 'exit 0' INT TERM

find_temperature() {
  local hw name path
  for hw in /sys/class/hwmon/hwmon*; do
    [ -r "${hw}/name" ] || continue
    name="$(cat "${hw}/name" 2>/dev/null || true)"
    case "${name}" in
      coretemp|k10temp|zenpower)
        for path in "${hw}"/temp*_input; do [ -r "${path}" ] && { echo "${path}"; return; }; done
        ;;
    esac
  done
  for path in /sys/class/hwmon/hwmon*/temp*_input; do [ -r "${path}" ] && { echo "${path}"; return; }; done
}

discover_fans() {
  local hw fan pwm
  for hw in /sys/class/hwmon/hwmon*; do
    for fan in "${hw}"/fan*_input; do
      [ -r "${fan}" ] || continue
      pwm="${fan%_input}"
      pwm="${pwm/fan/pwm}"
      [ -w "${pwm}" ] || continue
      PWM_PATHS+=("${pwm}")
    done
  done
}

TEMP_PATH="$(find_temperature || true)"
discover_fans
[ -n "${TEMP_PATH}" ] || { echo "No temperature sensor found" >&2; exit 1; }
[ "${#PWM_PATHS[@]}" -gt 0 ] || { echo "No writable PWM channels found" >&2; exit 1; }

if [ "${ENABLED}" != "1" ]; then exit 0; fi
for i in "${!PWM_PATHS[@]}"; do
  pwm="${PWM_PATHS[$i]}"
  [ -w "${pwm}_enable" ] || continue
  if [ "${SELECTED[$i]:-1}" = "1" ]; then
    printf '1\n' > "${pwm}_enable"
  fi
done

case "${ACTIVE_MODE}" in
  0) CURVE="${CURVE_FULL}" ;;
  2) CURVE="${CURVE_QUIET}" ;;
  *) CURVE="${CURVE_COOL}" ;;
esac
read -r T1 P1 T2 P2 T3 P3 <<< "${CURVE}"
read -ra SELECTED <<< "${FAN_SELECTION}"

calculate_pwm() {
  awk -v t="$1" -v t1="${T1}" -v p1="${P1}" -v t2="${T2}" -v p2="${P2}" -v t3="${T3}" -v p3="${P3}" 'BEGIN { if (t<=t1) p=p1; else if (t<=t2) p=p1+(p2-p1)*(t-t1)/(t2-t1); else if (t<=t3) p=p2+(p3-p2)*(t-t2)/(t3-t2); else p=p3; if(p<0)p=0;if(p>100)p=100; printf "%d", p*255/100 }'
}

while :; do
  temp="$(awk '{printf "%.1f", $1/1000}' "${TEMP_PATH}" 2>/dev/null || echo 0)"
  pwm_value="$(calculate_pwm "${temp}")"
  for i in "${!PWM_PATHS[@]}"; do
    [ "${SELECTED[$i]:-1}" = "1" ] || continue
    printf '%s\n' "${pwm_value}" > "${PWM_PATHS[$i]}" 2>/dev/null || true
  done
  sleep "${INTERVAL}"
done
