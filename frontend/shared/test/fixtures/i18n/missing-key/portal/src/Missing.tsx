declare function useT(): (key: string) => string

export function Missing() {
  const t = useT()
  return <p>{t('app.missing')}</p>
}
