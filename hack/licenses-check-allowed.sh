#!/bin/bash

# Copyright (c) 2022 RaptorML authors.
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

output=$(go-licenses check ./... 1>/dev/null 2>&1)
if [ -z "$output" ]; then
    echo -e "\033[0;32mLicense Check Success\033[0m"
    exit 0
else
    echo -e "\033[0;31mLicense Check Failed - You're importing a blocked license:\033[0m"
    echo "$output"
    exit 1
fi
