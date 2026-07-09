declare function useT(): (key: string) => string

export function Ok() {
  const t = useT()
  return <p>{t('app.hello')}</p>
}
