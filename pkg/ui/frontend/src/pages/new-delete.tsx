import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useCluster } from "@/contexts/use-cluster";
import { ServiceNames } from "@/lib/ring-utils";
import { findNodeName } from "@/lib/utils";
import { zodResolver } from "@hookform/resolvers/zod";
import { formatDuration, intervalToDuration } from "date-fns";
import debounce from "lodash/debounce";
import { AlertCircle, Loader2 } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { useForm, ControllerRenderProps } from "react-hook-form";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import * as z from "zod";
import DatePicker from "react-datepicker";
import "react-datepicker/dist/react-datepicker.css";
import { PageContainer } from "@/layout/page-container";
import { absolutePath } from "@/util";

const formSchema = z.object({
  tenant_id: z.string().min(1, "Tenant ID is required"),
  query: z.string().min(1, "Query is required"),
  start_time: z.date(),
  end_time: z
    .date()
    .refine(
      (date) => date > new Date(Date.now() - 7 * 24 * 60 * 60 * 1000),
      "End time must be after start time"
    ),
});

type FormValues = z.infer<typeof formSchema>;

// Add types for event handlers
type TextareaEvent = React.ChangeEvent<HTMLTextAreaElement>;

const NewDeleteRequest = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const [queryValidating, setQueryValidating] = useState(false);
  const { cluster } = useCluster();
  const nodeName = useMemo(() => {
    return findNodeName(cluster?.members, ServiceNames.compactor);
  }, [cluster?.members]);

  const form = useForm<z.infer<typeof formSchema>>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      tenant_id: "",
      query: "",
      start_time: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000), // 1 week ago
      end_time: new Date(), // now
    },
  });

  const validateQuery = useCallback(
    async (query: string, shouldFormat = false) => {
      if (!query.trim()) return;

      setQueryValidating(true);
      try {
        const response = await fetch(
          absolutePath(
            `/api/v1/proxy/${nodeName}/loki/api/v1/format_query?query=${query}`
          ),
          {
            method: "POST",
          }
        );

        const result = await response.json();

        if (!response.ok || result.status === "invalid-query") {
          throw new Error(result.error || "Invalid LogQL query");
        }

        // Clear any existing query errors when validation succeeds
        form.clearErrors("query");

        if (shouldFormat) {
          form.setValue("query", result.data);
        }
      } catch (error) {
        form.setError("query", {
          message:
            error instanceof Error ? error.message : "Invalid LogQL query",
        });
      } finally {
        setQueryValidating(false);
      }
    },
    [form, nodeName]
  );

  const debouncedValidate = useMemo(
    () => debounce((query: string) => validateQuery(query, false), 1000),
    [validateQuery]
  );

  const onSubmit = async (values: z.infer<typeof formSchema>) => {
    const params = new URLSearchParams();
    params.append("query", values.query);
    params.append(
      "start",
      Math.floor(values.start_time.getTime() / 1000).toString()
    );
    params.append(
      "end",
      Math.floor(values.end_time.getTime() / 1000).toString()
    );

    try {
      const response = await fetch(
        absolutePath(
          `/api/v1/proxy/${nodeName}/compactor/ui/api/v1/deletes?${params.toString()}`
        ),
        {
          method: "POST",
          headers: {
            "X-Scope-OrgID": values.tenant_id,
          },
        }
      );

      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || "Failed to create delete request");
      }

      await queryClient.invalidateQueries({ queryKey: ["deletes"] });
      navigate("/tenants/deletes");
    } catch (error) {
      console.error("Error creating delete request:", error);
      setError(
        error instanceof Error
          ? error.message
          : "Failed to create delete request"
      );
    }
  };

  const duration = useMemo(() => {
    const start = form.watch("start_time");
    const end = form.watch("end_time");
    return formatDuration(
      intervalToDuration({
        start,
        end,
      }),
      {
        format: ["years", "months", "weeks", "days", "hours", "minutes"],
        zero: false,
      }
    );
  }, [form]);

  return (
    <PageContainer>
      <Card>
        <CardHeader>
          <CardTitle>New Delete Request</CardTitle>
        </CardHeader>
        <CardContent>
          {error && (
            <Alert variant="destructive" className="mb-6">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>Error</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          <Form {...form}>
            <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-8">
              <FormField
                control={form.control}
                name="tenant_id"
                render={({
                  field,
                }: {
                  field: ControllerRenderProps<FormValues, "tenant_id">;
                }) => (
                  <FormItem>
                    <FormLabel>TENANT ID</FormLabel>
                    <FormControl>
                      <Input placeholder="Enter tenant ID" {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="query"
                render={({
                  field,
                }: {
                  field: ControllerRenderProps<FormValues, "query">;
                }) => (
                  <FormItem>
                    <FormLabel>LOGQL QUERY</FormLabel>
                    <FormControl>
                      <div className="relative">
                        <Textarea
                          placeholder='{app="example"}'
                          className="font-mono"
                          {...field}
                          onChange={(e: TextareaEvent) => {
                            field.onChange(e);
                            debouncedValidate(e.target.value);
                          }}
                          onBlur={async (e: TextareaEvent) => {
                            field.onBlur();
                            if (e.target.value) {
                              await validateQuery(e.target.value, true);
                            }
                          }}
                        />
                        {queryValidating && (
                          <div className="absolute right-3 top-3">
                            <Loader2 className="h-5 w-5 animate-spin" />
                          </div>
                        )}
                      </div>
                    </FormControl>
                    <FormDescription>
                      Enter a LogQL query with labels in curly braces
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <div className="grid grid-cols-3 gap-8">
                <FormField
                  control={form.control}
                  name="start_time"
                  render={({
                    field,
                  }: {
                    field: ControllerRenderProps<FormValues, "start_time">;
                  }) => (
                    <FormItem>
                      <FormLabel>START TIME</FormLabel>
                      <FormControl>
                        <DatePicker
                          selected={field.value}
                          onChange={field.onChange}
                          showTimeSelect
                          timeFormat="HH:mm"
                          timeIntervals={15}
                          dateFormat="yyyy-MM-dd HH:mm"
                          className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="end_time"
                  render={({
                    field,
                  }: {
                    field: ControllerRenderProps<FormValues, "end_time">;
                  }) => (
                    <FormItem>
                      <FormLabel>END TIME</FormLabel>
                      <FormControl>
                        <DatePicker
                          selected={field.value}
                          onChange={field.onChange}
                          showTimeSelect
                          timeFormat="HH:mm"
                          timeIntervals={15}
                          dateFormat="yyyy-MM-dd HH:mm"
                          minDate={form.watch("start_time")}
                          className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <div className="space-y-2">
                  <FormLabel>DURATION</FormLabel>
                  <div className="h-10 flex items-center">
                    <span className="text-sm text-muted-foreground">
                      {duration}
                    </span>
                  </div>
                </div>
              </div>

              <div className="flex justify-end space-x-3 pt-6 border-t">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => navigate("/tenants/deletes")}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={
                    !form.formState.isValid || form.formState.isSubmitting
                  }
                >
                  {form.formState.isSubmitting
                    ? "Creating..."
                    : "Create Delete Request"}
                </Button>
              </div>
            </form>
          </Form>
        </CardContent>
      </Card>
    </PageContainer>
  );
};

export default NewDeleteRequest;
