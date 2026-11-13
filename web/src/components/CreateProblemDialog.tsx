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

import { useEffect, useState } from "react";
import { toast } from "sonner";
import { ApiError, type Schemas } from "@/api/client";
import { useCategories, useCreateProblem, useSimilarProblems } from "@/api/hooks";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { NativeSelect } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { fieldErrors, problemInputSchema } from "@/lib/schemas";

interface Props {
  open: boolean;
  initialTitle: string;
  onOpenChange: (open: boolean) => void;
  onCreated: (p: Schemas["Problem"]) => void;
  onUseExisting?: (p: { id: string; title: string }) => void;
}

export function CreateProblemDialog({ open, initialTitle, onOpenChange, onCreated, onUseExisting }: Props) {
  const categories = useCategories();
  const create = useCreateProblem();
  const [title, setTitle] = useState(initialTitle);
  const [categoryId, setCategoryId] = useState("");
  const [description, setDescription] = useState("");
  const [alias, setAlias] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const similar = useSimilarProblems(title);

  useEffect(() => {
    if (open) {
      setTitle(initialTitle);
      setErrors({});
    }
  }, [open, initialTitle]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const parsed = problemInputSchema.safeParse({
      title,
      category_id: categoryId,
      description: description || undefined,
      alias: alias.trim() ? alias.trim().toLowerCase() : null,
    });
    if (!parsed.success) {
      setErrors(fieldErrors(parsed.error));
      return;
    }
    try {
      const p = await create.mutateAsync(parsed.data);
      toast.success(`Created “${p.title}”`);
      onCreated(p);
      onOpenChange(false);
      setDescription("");
      setAlias("");
    } catch (err) {
      if (err instanceof ApiError) {
        setErrors(Object.fromEntries(err.fields.map((f) => [f.field, f.message])));
        if (!err.fields.length) toast.error(err.message);
      } else toast.error("Could not reach the server. Try again when you're online.");
    }
  }

  const suggestions = similar.data?.items ?? [];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New problem</DialogTitle>
          <DialogDescription>
            A problem is a recurring source of friction. Anyone can add one — no approval needed.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="np-title">Title</Label>
            <Input
              id="np-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              aria-invalid={!!errors.title}
              autoFocus
              placeholder="e.g. Corporate VPN disconnects"
            />
            {errors.title && <p className="text-sm text-destructive">{errors.title}</p>}
          </div>
          {suggestions.length > 0 && (
            <Alert variant="info">
              <AlertTitle>Is it one of these?</AlertTitle>
              <AlertDescription>
                <ul className="mt-1 space-y-1">
                  {suggestions.map((s) => (
                    <li key={s.id} className="flex items-center justify-between gap-2">
                      <span>{s.title}</span>
                      {onUseExisting && (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            onUseExisting({ id: s.id, title: s.title });
                            onOpenChange(false);
                          }}
                        >
                          Use this
                        </Button>
                      )}
                    </li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          )}
          <div className="space-y-1.5">
            <Label htmlFor="np-category">Category</Label>
            <NativeSelect
              id="np-category"
              value={categoryId}
              onChange={(e) => setCategoryId(e.target.value)}
              aria-invalid={!!errors.category_id}
            >
              <option value="">Choose…</option>
              {categories.data?.items.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </NativeSelect>
            {errors.category_id && <p className="text-sm text-destructive">{errors.category_id}</p>}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="np-alias">
              Short alias <span className="font-normal text-muted-foreground">(optional, e.g. “vpn”)</span>
            </Label>
            <Input
              id="np-alias"
              value={alias}
              onChange={(e) => setAlias(e.target.value)}
              aria-invalid={!!errors.alias}
            />
            {errors.alias && <p className="text-sm text-destructive">{errors.alias}</p>}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="np-desc">
              Description <span className="font-normal text-muted-foreground">(optional)</span>
            </Label>
            <Textarea id="np-desc" value={description} onChange={(e) => setDescription(e.target.value)} />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              Create problem
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
