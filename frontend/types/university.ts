import { CountryCode } from '../js/features/settings/data/countries-list'

export type University = {
  id: number
  name: string
  country_code: CountryCode
  departments: string[]
}
