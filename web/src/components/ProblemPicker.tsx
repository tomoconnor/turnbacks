/**
 * Copyright (c) 2026 Tom O'Connor <tom@twinhelix.org>
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

import { Plus } from "lucide-react";
import { forwardRef, useDeferredValue, useMemo, useState } from "react";
import { useAllActiveProblems, useProblemSearch } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { useOnline } from "@/offline/useOutbox";
import { useSession } from "@/lib/session";

export interface PickedProblem {
  id: string;
  title: string;
}

interface Props {
  onPick: (p: PickedProblem) => void;
  onCreate: (title: string) => void;
}

/**
 * Searchable problem selection. Searches on the server (alias, title,
 * description and fuzzy similarity); falls back to the cached problem list
 * when offline.
 */
export const ProblemPicker = forwardRef<HTMLInputElement, Props>(function ProblemPicker(
  { onPick, onCreate },
  ref,
) {
  const [q, setQ] = useState("");
  const deferred = useDeferredValue(q.trim());
  const online = useOnline();
  const { offline } = useSession();
  const useLocal = !online || offline;
  const remote = useProblemSearch({ q: deferred || undefined, limit: 8 });
  const all = useAllActiveProblems();

  const results = useMemo(() => {
    if (!useLocal && remote.data) return remote.data.items;
    const items = all.data?.items ?? [];
    const needle = deferred.toLowerCase();
    return items
      .filter((p) => !needle || p.title.toLowerCase().includes(needle) || (p.alias ?? "").startsWith(needle))
      .slice(0, 8);
  }, [useLocal, remote.data, all.data, deferred]);

  const active = deferred.length > 0;

  return (
    <div className="space-y-2">
      <div>
        <Command shouldFilter={false} className="rounded-md border">
          <CommandInput
            ref={ref}
            value={q}
            onValueChange={setQ}
            placeholder="Search problems or aliases…  ( / )"
            aria-label="Search problems"
          />
          {/* Always rendered so the combobox's aria-controls target exists. */}
          <CommandList className={active ? undefined : "hidden"}>
            {active && <CommandEmpty>No matching problems.</CommandEmpty>}
            {active && results.length > 0 && (
              <CommandGroup heading={useLocal ? "Problems (offline copy)" : "Problems"}>
                {results.map((p) => (
                  <CommandItem
                    key={p.id}
                    value={p.id}
                    onSelect={() => {
                      onPick({ id: p.id, title: p.title });
                      setQ("");
                    }}
                  >
                    <span className="flex-1 truncate">{p.title}</span>
                    {p.alias && <Badge variant="outline">{p.alias}</Badge>}
                    <span className="hidden text-xs text-muted-foreground sm:inline">{p.category_name}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {deferred.length >= 3 && !useLocal && (
              <CommandGroup>
                <CommandItem value="__create__" onSelect={() => onCreate(q.trim())}>
                  <Plus /> Create “{q.trim()}” as a new problem
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </div>
    </div>
  );
});
