/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { expect, test } from 'vitest'

import {
  formatPricingNumber,
  formatPricingNumber2dp,
} from '../pricing-format'

test.each([
  [3, '3.00'],
  [0.5, '0.50'],
  [0.149, '0.15'],
  [28, '28.00'],
  [0.1 + 0.2, '0.30'],
  [0, '0.00'],
] as const)('formatPricingNumber2dp(%s) -> %s', (input, expected) => {
  expect(formatPricingNumber2dp(input)).toBe(expected)
})

test.each([
  [0.0049, '0.0049'],
  [0.000001, '0.000001'],
] as const)(
  'formatPricingNumber2dp(%s) keeps precision below 0.01 -> %s',
  (input, expected) => {
    expect(formatPricingNumber2dp(input)).toBe(expected)
  }
)

test('formatPricingNumber2dp handles non-numeric input like formatPricingNumber', () => {
  expect(formatPricingNumber2dp('')).toBe('')
  expect(formatPricingNumber2dp(null)).toBe('')
  expect(formatPricingNumber2dp('abc')).toBe('')
  expect(formatPricingNumber2dp('2.5')).toBe('2.50')
  // The legacy formatter keeps its full-precision behavior.
  expect(formatPricingNumber(0.149)).toBe('0.149')
})
