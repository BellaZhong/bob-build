# Copyright 2018-2019 Arm Limited.
# SPDX-License-Identifier: Apache-2.0
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import os
import sys

# The config system depends on the `ply` parser generator. On Android, this may
# come as a prebuilt, but may _not_ automatically be added to PYTHONPATH. If
# we're on Android (tested by checking for `envsetup.mk`), then add the `ply`
# prebuilt to `sys.path`:
if os.path.isfile("build/make/core/envsetup.mk"):
    if os.path.isdir("external/ply/ply"):
        sys.path.insert(0, "external/ply/ply")

from config_system.general import \
    can_enable, \
    format_dependency_list, \
    get_config, \
    get_config_bool, \
    get_config_int, \
    get_config_list, \
    get_config_string, \
    get_mconfig_dir, \
    get_options_depending_on, \
    get_options_selecting, \
    init_config, \
    read_config, \
    set_config
