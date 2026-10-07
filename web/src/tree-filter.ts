/** The items whose text matches the query with their whole subtree, and a copy of each ancestor holding only its matching children. */
export function filterItems<T extends { children: T[] }>(items: T[], query: string, text: (item: T) => string): T[] {
  if (!query.trim()) return items;
  const needle = query.trim().toLowerCase();
  return items.flatMap((item) => {
    if (text(item).toLowerCase().includes(needle)) return [item];
    const children = filterItems(item.children, query, text);
    return children.length ? [{ ...item, children }] : [];
  });
}
