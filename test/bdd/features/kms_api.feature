#
# Copyright SecureKey Technologies Inc. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#

@all
@kms
Feature: KMS operations

  Scenario: User creates a key
    Given Key Server is running on "localhost" port "8070"
      And User has created a keystore on the server
    When  User sends an HTTP POST to "http://{keystoreEndpoint}/keys" to create a key of "ED25519" type
    Then  User gets a response with HTTP 201 Created and Location with a valid URL for the newly created key