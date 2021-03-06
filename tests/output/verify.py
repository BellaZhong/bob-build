#!/bin/env python

# Copyright 2019 Arm Limited.
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

from __future__ import print_function

import argparse
import sys
import os

parser = argparse.ArgumentParser(description='Test generator.')
parser.add_argument('--out')
parser.add_argument('--expected')

args = parser.parse_args()

if os.path.basename(args.out) != args.expected:
    print("Output from generation: {} but expected: {}".format(args.out, args.expected))
    sys.exit(1)
